package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"mpn/internal/distro"
	"mpn/internal/installer"
	"mpn/internal/mail"
	"mpn/internal/security"
	"mpn/internal/store"
	"mpn/internal/svcport"
)

// The settings that hold the ports chosen on the Firewall page, and the change of SSH or of the panel that waits to be finished.
const (
	settingPMAPort     = "port_pma"       // the database browser; empty is the one after the panel's
	settingWebmailPort = "port_webmail"   // the web mail; empty is the second one after the panel's
	settingSSHOld      = "port_ssh_old"   // while a change of SSH waits: the ports that close when it is finished
	settingPanelOld    = "port_panel_old" // while a change of the panel waits: the old port
	settingPortSince   = "port_since"     // Unix seconds when the waiting change was made
)

// portRevertAfter is how long a change of SSH or of the panel may wait. After that the timer mpn-port-revert puts the old port
// back, so a new port that nobody can reach (closed by the provider, say) never stays the only way in.
const portRevertAfter = 10 * time.Minute

const portRevertUnit = "mpn-port-revert"

func portUsage() {
	fmt.Fprintln(os.Stderr, "usage: mpn port <status|set|finish|cancel|ssh-addr> [flags]   (set -h for the flags)")
	os.Exit(2)
}

// sshListenPorts are the ports sshd listens on, as lockedPorts finds them for the firewall.
func sshListenPorts() []int { return lockedPorts().SSH }

// pmaPortNow is the port the database browser listens on, 0 when it is not installed.
func pmaPortNow() int {
	if p := listenPorts(pmaFiles()...); len(p) > 0 {
		return p[0]
	}
	return 0
}

// webmailPortOf is the port of the web mail: the one chosen on the Firewall page, or the second after the panel's.
func webmailPortOf(st *store.Store) int {
	if v, _ := st.Setting(settingWebmailPort, ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	if pp := panelPortOf(); pp > 0 {
		return mail.WebmailPort(pp)
	}
	return 0
}

// pmaPortSetting is the port chosen for the database browser, 0 when none was.
func pmaPortSetting(st *store.Store) int {
	v, _ := st.Setting(settingPMAPort, "")
	n, _ := strconv.Atoi(v)
	return n
}

func currentPorts(st *store.Store) svcport.Status {
	s := svcport.Status{SSH: sshListenPorts(), PMA: pmaPortNow(), Webmail: webmailPortOf(st)}
	if p := listenPorts(panelNginx); len(p) > 0 {
		s.Panel = p[0]
	}
	if v, _ := st.Setting(settingWebmail, "off"); v == "on" {
		s.WebmailOn = true
	}
	if v, _ := st.Setting(settingSSHOld, ""); v != "" {
		s.SSHOld = svcport.List(v)
	}
	if v, _ := st.Setting(settingPanelOld, ""); v != "" {
		// While a change of the panel waits, nginx listens on both.
		if old, _ := strconv.Atoi(v); old > 0 {
			s.Panel, s.PanelNew = old, panelNewPort(old)
		}
	}
	return s
}

// panelNewPort is the port the panel's nginx file listens on besides old, 0 when there is none. listenPorts sorts what it finds, so
// the new port is told apart from the old one by value, not by its place in the file.
func panelNewPort(old int) int {
	for _, p := range listenPorts(panelNginx) {
		if p != old {
			return p
		}
	}
	return 0
}

// portFree checks that nothing on the machine holds port, apart from the service itself. Ports reserved for app sites count too.
func portFree(st *store.Store, service string, port int) error {
	cur := currentPorts(st)
	if who, ok := cur.InUse()[port]; ok {
		if who == service {
			return fmt.Errorf("%s already uses port %d", service, port)
		}
		return fmt.Errorf("port %d is the port of %s", port, who)
	}
	if sites, err := st.ListSites(); err == nil {
		for _, s := range sites {
			if s.AppPort == port {
				return fmt.Errorf("port %d is the port of the website %s", port, s.Name)
			}
		}
	}
	for _, l := range svcport.Listeners(out(5*time.Second, "ss", "-ltnH")) {
		if l.Port == port {
			return fmt.Errorf("port %d is already in use by another program on this server", port)
		}
	}
	return nil
}

// runPort changes the ports of SSH, of the panel, of the database browser and of the web mail, and says which port the panel should
// use to reach SSH.
func runPort(args []string) {
	if len(args) == 0 {
		portUsage()
	}
	sub := args[0]
	fs := flag.NewFlagSet("port "+sub, flag.ExitOnError)
	db := fs.String("db", defaultDB, "database path")
	asJSON := fs.Bool("json", false, "status: print JSON")
	service := fs.String("service", "", "set, finish, cancel: ssh, panel, pma or webmail")
	port := fs.Int("port", 0, "set: the new port")
	auto := fs.Bool("auto", false, "cancel: the timer put the old port back (no one asked)")
	apply := fs.Bool("apply", false, "change this machine (Linux, root); otherwise only check")
	_ = fs.Parse(args[1:])

	if sub == "ssh-addr" {
		// The panel dials this address for the terminal and the file manager. It is printed for the panel's account, which cannot
		// run sshd -T itself.
		fmt.Println(svcport.DialAddr(sshListenPorts(), svcport.Listeners(out(5*time.Second, "ss", "-ltnH"))))
		return
	}
	st := openStore(*db)
	defer st.Close()

	switch sub {
	case "status":
		s := currentPorts(st)
		if *asJSON {
			printJSON(s)
			return
		}
		fmt.Printf("ssh %v  panel %d  phpmyadmin %d  webmail %d\n", s.SSH, s.Panel, s.PMA, s.Webmail)
		if len(s.SSHOld) > 0 {
			fmt.Printf("waiting: SSH moves from %v; finish with mpn port finish -service ssh -apply\n", s.SSHOld)
		}
		if s.PanelNew > 0 {
			fmt.Printf("waiting: the panel moves to %d; finish with mpn port finish -service panel -apply\n", s.PanelNew)
		}
	case "set":
		fail(svcport.Valid(*service, *port))
		if svcport.TwoStep(*service) && waiting(st) != "" {
			fail(fmt.Errorf("port-waiting: the change of the port of %s is not finished yet; finish or cancel it first", waiting(st)))
		}
		if *service == svcport.PMA && pmaPortNow() == 0 {
			fail(errors.New("phpMyAdmin is not installed on this machine"))
		}
		if (*service == svcport.Panel || *service == svcport.PMA || *service == svcport.Webmail) && panelPortOf() == 0 {
			fail(errors.New("the web panel is not installed on this machine"))
		}
		fail(portFree(st, *service, *port))
		if !*apply {
			fmt.Printf("# checked only. Port %d is free for %s. Add -apply to change it.\n", *port, *service)
			return
		}
		requireLinuxRoot()
		switch *service {
		case svcport.SSH:
			fail(sshPortStart(st, *port))
		case svcport.Panel:
			fail(panelPortStart(st, *port))
		case svcport.PMA:
			fail(pmaPortChange(st, *port))
		case svcport.Webmail:
			fail(webmailPortChange(st, *port))
		}
	case "finish", "cancel":
		if !svcport.TwoStep(*service) {
			fail(errors.New("only a change of ssh or of the panel is finished or cancelled"))
		}
		if waiting(st) != *service {
			if *auto {
				return // finished by hand before the timer ran
			}
			fail(fmt.Errorf("no change of the port of %s is waiting", *service))
		}
		if !*apply {
			fmt.Printf("# checked only. Add -apply to %s the change of %s.\n", sub, *service)
			return
		}
		requireLinuxRoot()
		var err error
		switch {
		case *service == svcport.SSH && sub == "finish":
			err = sshPortFinish(st)
		case *service == svcport.SSH:
			err = sshPortCancel(st)
		case sub == "finish":
			err = panelPortFinish(st)
		default:
			err = panelPortCancel(st)
		}
		fail(err)
	default:
		portUsage()
	}
}

// waiting names the service whose change of port waits to be finished, "" when none does.
func waiting(st *store.Store) string {
	if v, _ := st.Setting(settingSSHOld, ""); v != "" {
		return svcport.SSH
	}
	if v, _ := st.Setting(settingPanelOld, ""); v != "" {
		return svcport.Panel
	}
	return ""
}

func clearWaiting(st *store.Store) error {
	for _, k := range []string{settingSSHOld, settingPanelOld, settingPortSince} {
		if err := st.SetSetting(k, ""); err != nil {
			return err
		}
	}
	_, _ = sh(5*time.Second, "systemctl", "stop", portRevertUnit+".timer")
	return nil
}

// armRevert starts the timer that cancels the change when nobody finishes it.
func armRevert(service string) error {
	_, _ = sh(5*time.Second, "systemctl", "stop", portRevertUnit+".timer", portRevertUnit+".service")
	_, _ = sh(5*time.Second, "systemctl", "reset-failed", portRevertUnit+".timer", portRevertUnit+".service")
	_, err := sh(10*time.Second, "systemd-run", "--quiet", "--collect", "--unit="+portRevertUnit,
		"--on-active="+strconv.Itoa(int(portRevertAfter.Seconds())), "--timer-property=AccuracySec=5s",
		"/usr/local/bin/mpn", "port", "cancel", "-service", service, "-auto", "-apply")
	return err
}

// afterPortChange brings the parts that follow the ports up to date: the firewall (its always-open ports are read from sshd and nginx
// at every load) and fail2ban (its jails name the ports).
func afterPortChange(st *store.Store) error {
	if on, _ := st.FirewallOn(); on {
		rules, err := st.FirewallRules()
		if err != nil {
			return err
		}
		if err := runPlanQuiet(loadPlan(rules)); err != nil {
			return err
		}
	}
	if v, _ := st.Setting(settingF2B, "off"); v == "on" && fileExists(security.JailFile) {
		l := lockedPorts()
		jail, err := security.JailConfig(l.SSH, l.Panel, ignoreList(st))
		if err != nil {
			return err
		}
		return runPlanQuiet(installer.Plan{{Name: "fail2ban jails", Actions: []installer.Action{
			installer.WriteFile(security.JailFile, jail, 0o644),
			installer.Run("fail2ban-client", "-t"),
			installer.Run("systemctl", "reload-or-restart", "fail2ban"),
		}}})
	}
	return nil
}

// waitListen waits until something listens on port, or gives up after a few seconds.
func waitListen(port int) bool {
	for i := 0; i < 20; i++ {
		for _, l := range svcport.Listeners(out(5*time.Second, "ss", "-ltnH")) {
			if l.Port == port {
				return true
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// ---- SSH -----------------------------------------------------------------------------------------------------------------------

func sshApply(ports []int) error {
	script, err := svcport.SSHPortScript(ports)
	if err != nil {
		return err
	}
	acts := []installer.Action{}
	if distro.IsEL() {
		for _, p := range ports {
			acts = append(acts, installer.Run("sh", "-c", svcport.SELinuxPortScript("ssh_port_t", p)))
		}
	}
	acts = append(acts, installer.Run("sh", "-c", script), installer.Run("sh", "-c", svcport.SSHRestartScript))
	return runPlanQuiet(installer.Plan{{Name: "ports of SSH", Actions: acts}})
}

// sshPortStart adds the new port next to the ports sshd listens on now. The firewall opens it before sshd takes it, so the person
// can try it at once.
func sshPortStart(st *store.Store, port int) error {
	old := sshListenPorts()
	if err := st.SetSetting(settingSSHOld, svcport.Join(old)); err != nil {
		return err
	}
	_ = st.SetSetting(settingPortSince, strconv.FormatInt(time.Now().Unix(), 10))
	if err := sshApply(svcport.List(svcport.Join(append(old, port)))); err != nil {
		_ = clearWaiting(st)
		return err
	}
	if !waitListen(port) {
		_ = sshApply(old)
		_ = clearWaiting(st)
		return fmt.Errorf("sshd did not start listening on port %d; the old ports %v are kept", port, old)
	}
	if err := afterPortChange(st); err != nil {
		return err
	}
	if err := armRevert(svcport.SSH); err != nil {
		fmt.Fprintf(os.Stderr, "warning: the timer that puts the old port back could not be started: %v\n", err)
	}
	fmt.Printf("SSH now listens on %d as well as %v. Log in on port %d, then finish the change; until then both stay open (at most %d minutes).\n",
		port, old, port, int(portRevertAfter.Minutes()))
	return nil
}

func sshPortFinish(st *store.Store) error {
	cur, old := sshListenPorts(), svcport.List(mustSetting(st, settingSSHOld))
	var keep []int
	for _, p := range cur {
		if !containsInt(old, p) {
			keep = append(keep, p)
		}
	}
	if len(keep) == 0 {
		return errors.New("sshd listens on no new port; cancel the change instead")
	}
	if err := sshApply(keep); err != nil {
		return err
	}
	if err := clearWaiting(st); err != nil {
		return err
	}
	if err := afterPortChange(st); err != nil {
		return err
	}
	fmt.Printf("SSH listens on %v only. Sessions that are open stay open.\n", keep)
	return nil
}

func sshPortCancel(st *store.Store) error {
	old := svcport.List(mustSetting(st, settingSSHOld))
	if len(old) == 0 {
		old = []int{22}
	}
	if err := sshApply(old); err != nil {
		return err
	}
	if err := clearWaiting(st); err != nil {
		return err
	}
	if err := afterPortChange(st); err != nil {
		return err
	}
	fmt.Printf("SSH is back on %v.\n", old)
	return nil
}

// ---- the panel -----------------------------------------------------------------------------------------------------------------

// panelNginxPlan writes the panel's server block on ports and makes nginx take it. On EL the new ports get the label nginx may
// listen on first.
func panelNginxPlan(ports ...int) installer.Plan {
	acts := []installer.Action{}
	if distro.IsEL() {
		for _, p := range ports {
			acts = append(acts, installer.Run("sh", "-c", svcport.SELinuxPortScript("http_port_t", p)))
		}
	}
	acts = append(acts,
		installer.Run("cp", "-p", panelNginx, panelNginx+".mpn-old"),
		installer.WriteFile(panelNginx, installer.PanelNginx(ports...), 0o644),
		installer.Run("sh", "-c", "nginx -t || { cp -p "+panelNginx+".mpn-old "+panelNginx+"; exit 1; }"),
		installer.ReloadNginx(),
		installer.Run("rm", "-f", panelNginx+".mpn-old"),
	)
	return installer.Plan{{Name: "port of the panel", Actions: acts}}
}

// panelPortStart makes nginx answer the panel on the new port as well as the old one. The new port comes first in the file, so
// once the change is finished it is the one everything reads.
func panelPortStart(st *store.Store, port int) error {
	old := panelPortOf()
	if err := st.SetSetting(settingPanelOld, strconv.Itoa(old)); err != nil {
		return err
	}
	_ = st.SetSetting(settingPortSince, strconv.FormatInt(time.Now().Unix(), 10))
	// The database browser and the web mail keep their ports: they are written down now if they only followed the panel's.
	if pmaPortNow() > 0 && pmaPortSetting(st) == 0 {
		_ = st.SetSetting(settingPMAPort, strconv.Itoa(pmaPortNow()))
	}
	if v, _ := st.Setting(settingWebmailPort, ""); v == "" {
		_ = st.SetSetting(settingWebmailPort, strconv.Itoa(mail.WebmailPort(old)))
	}
	if err := runPlanQuiet(panelNginxPlan(port, old)); err != nil {
		_ = clearWaiting(st)
		return err
	}
	if err := afterPortChange(st); err != nil {
		return err
	}
	if err := armRevert(svcport.Panel); err != nil {
		fmt.Fprintf(os.Stderr, "warning: the timer that puts the old port back could not be started: %v\n", err)
	}
	fmt.Printf("The panel now answers on %d as well as %d. Open it on port %d and finish the change there; until then both stay open (at most %d minutes).\n",
		port, old, port, int(portRevertAfter.Minutes()))
	return nil
}

// panelPortFinish closes the old port and writes the new one wherever the panel's port is written: the sign-out page of the database
// browser (it sends the visitor back to the panel), the browser's sign-in redirect, and the firewall and fail2ban.
func panelPortFinish(st *store.Store) error {
	old, _ := strconv.Atoi(mustSetting(st, settingPanelOld))
	port := panelNewPort(old)
	if port == 0 {
		return errors.New("the panel listens on no new port; cancel the change instead")
	}
	if err := runPlanQuiet(panelNginxPlan(port)); err != nil {
		return err
	}
	if pp := pmaPortNow(); pp > 0 {
		if err := runPlanQuiet(installer.Plan{{Name: "phpMyAdmin follows the panel", Actions: []installer.Action{
			installer.WriteFile(installer.PMAConfFile, installer.PMANginx(port, pp), 0o644),
			installer.Run("nginx", "-t"),
			installer.ReloadNginx(),
		}}}); err != nil {
			return err
		}
	}
	if err := clearWaiting(st); err != nil {
		return err
	}
	if err := syncFront(st); err != nil { // the pages of the database browser that name the panel's port
		return err
	}
	if err := afterPortChange(st); err != nil {
		return err
	}
	fmt.Printf("The panel answers on port %d only.\n", port)
	return nil
}

func panelPortCancel(st *store.Store) error {
	old, _ := strconv.Atoi(mustSetting(st, settingPanelOld))
	if old == 0 {
		return errors.New("the old port of the panel is not known")
	}
	if err := runPlanQuiet(panelNginxPlan(old)); err != nil {
		return err
	}
	if err := clearWaiting(st); err != nil {
		return err
	}
	if err := afterPortChange(st); err != nil {
		return err
	}
	fmt.Printf("The panel is back on port %d only.\n", old)
	return nil
}

// ---- phpMyAdmin and the web mail ----------------------------------------------------------------------------------------------

// pmaPortChange moves the database browser. The panel opens it on the port given to mpn-web with -pma-port, so the unit is changed
// and the panel restarts a few seconds after this answer reaches the page.
func pmaPortChange(st *store.Store, port int) error {
	pp := panelPortOf()
	acts := []installer.Action{}
	if distro.IsEL() {
		acts = append(acts, installer.Run("sh", "-c", svcport.SELinuxPortScript("http_port_t", port)))
	}
	acts = append(acts,
		installer.Run("cp", "-p", installer.PMAConfFile, installer.PMAConfFile+".mpn-old"),
		installer.WriteFile(installer.PMAConfFile, installer.PMANginx(pp, port), 0o644),
		installer.Run("sh", "-c", "nginx -t || { cp -p "+installer.PMAConfFile+".mpn-old "+installer.PMAConfFile+"; exit 1; }"),
		installer.ReloadNginx(),
		installer.Run("rm", "-f", installer.PMAConfFile+".mpn-old"),
		installer.Run("sed", "-i", `s| -pma-port [0-9]*| -pma-port `+strconv.Itoa(port)+`|`, installer.WebUnitFile),
		installer.Run("systemctl", "daemon-reload"),
		installer.Run("systemd-run", "--quiet", "--collect", "--on-active=4", "--unit=mpn-web-restart", "systemctl", "restart", "mpn-web"),
	)
	if err := runPlanQuiet(installer.Plan{{Name: "port of phpMyAdmin", Actions: acts}}); err != nil {
		return err
	}
	if err := st.SetSetting(settingPMAPort, strconv.Itoa(port)); err != nil {
		return err
	}
	if err := afterPortChange(st); err != nil {
		return err
	}
	fmt.Printf("phpMyAdmin is on port %d. The panel restarts in a few seconds to open it there.\n", port)
	return nil
}

// webmailPortChange moves the web mail. While it is off only the port is kept, for when it is turned on.
func webmailPortChange(st *store.Store, port int) error {
	if _, err := mail.WebmailNginxOn(port); err != nil {
		return err
	}
	if v, _ := st.Setting(settingWebmail, "off"); v == "on" {
		conf, _ := mail.WebmailNginxOn(port)
		acts := []installer.Action{}
		if distro.IsEL() {
			acts = append(acts, installer.Run("sh", "-c", svcport.SELinuxPortScript("http_port_t", port)))
		}
		acts = append(acts,
			installer.Run("cp", "-p", mail.WebmailNginxFile, mail.WebmailNginxFile+".mpn-old"),
			installer.WriteFile(mail.WebmailNginxFile, conf, 0o644),
			installer.Run("sh", "-c", "nginx -t || { cp -p "+mail.WebmailNginxFile+".mpn-old "+mail.WebmailNginxFile+"; exit 1; }"),
			installer.ReloadNginx(),
			installer.Run("rm", "-f", mail.WebmailNginxFile+".mpn-old"),
		)
		if err := runPlanQuiet(installer.Plan{{Name: "port of the web mail", Actions: acts}}); err != nil {
			return err
		}
	}
	if err := st.SetSetting(settingWebmailPort, strconv.Itoa(port)); err != nil {
		return err
	}
	if v, _ := st.Setting(settingWebmail, "off"); v == "on" {
		if err := webmailFirewall(st, true, port); err != nil {
			return err
		}
		fmt.Printf("The web mail is on port %d.\n", port)
		return nil
	}
	fmt.Printf("The web mail will use port %d when it is turned on.\n", port)
	return nil
}

func mustSetting(st *store.Store, key string) string {
	v, _ := st.Setting(key, "")
	return strings.TrimSpace(v)
}

func containsInt(list []int, n int) bool {
	for _, x := range list {
		if x == n {
			return true
		}
	}
	return false
}
