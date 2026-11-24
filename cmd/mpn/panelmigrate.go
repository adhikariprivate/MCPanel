package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"mpn/internal/distro"
	"mpn/internal/installer"
	"mpn/internal/osinfo"
	"mpn/internal/site"
	"mpn/internal/store"
	"mpn/internal/webauth"
)

// panelMigratePath performs the runtime cleanup before Store.Open is allowed to run schema migrations. This ordering is
// intentional: an interrupted cleanup leaves the database readable by the previous release and never loses site data.
func panelMigratePath(dbPath string, apply bool) {
	startingSchema, err := store.SchemaVersionAt(dbPath)
	if err != nil {
		fail(fmt.Errorf("checking database schema: %w", err))
	}
	if apply && startingSchema < store.CurrentSchemaVersion() {
		requireLinuxRoot()
		fail(requireMpnServicesStopped())
	}
	legacy, err := store.LegacyRuntimeSites(dbPath)
	if err != nil {
		fail(fmt.Errorf("checking legacy Node.js/Python sites: %w", err))
	}
	cleaned := len(legacy) > 0
	var cleanup installer.Plan
	if cleaned {
		plan, err := runtimeRemovalPlan(legacy)
		if err != nil {
			fail(err)
		}
		cleanup = append(cleanup, plan...)
	}
	keyPlan, err := obsoleteNodeKeyPlan()
	if err != nil {
		fail(err)
	}
	cleanup = append(cleanup, keyPlan...)
	if !apply && len(cleanup) > 0 {
		fmt.Print(cleanup)
		fmt.Println("# Site home directories and database contents are preserved; add -apply after reviewing this cleanup plan.")
	}
	if apply && len(cleanup) > 0 {
		requireLinuxRoot()
		for _, step := range cleanup {
			if err := runPlanQuiet(installer.Plan{step}); err != nil {
				fail(fmt.Errorf("cleaning generated runtime configuration failed; database records and site home directories were preserved: %w", err))
			}
		}
	}
	if !apply {
		panelMigrate(nil, false)
		return
	}
	var st *store.Store
	var openErr error
	if cleaned {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			fail(err)
		}
		st, openErr = store.OpenAfterRuntimeCleanup(dbPath)
	} else {
		st, openErr = store.Open(dbPath)
	}
	if openErr != nil {
		if version, err := store.SchemaVersionAt(dbPath); err == nil && version > startingSchema {
			panelRuntimeMigrationCommitted = true
		}
		fail(openErr)
	}
	if store.CurrentSchemaVersion() > startingSchema {
		panelRuntimeMigrationCommitted = true
		fmt.Println("database schema migration committed; older MCPN binaries may not understand it")
	}
	if cleaned {
		fail(removeLegacySiteGrants(filepath.Join(filepath.Dir(defaultAccount), "delegated-access.json"), legacy))
	}
	defer st.Close()
	panelMigrate(st, apply)
}

func obsoleteNodeKeyPlan() (installer.Plan, error) {
	path := "/etc/mpn/node-release-keys.gpg"
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file; resolve it manually before migration", path)
	}
	script := fmt.Sprintf(`set -eu
file=%q
[ ! -L "$file" ] || { echo "refusing symlink keyring $file" >&2; exit 1; }
rm -f -- "$file"`, path)
	return installer.Plan{{Name: "remove obsolete MCPN Node.js release keyring " + path, Actions: []installer.Action{installer.Run("sh", "-c", script)}}}, nil
}

func removeLegacySiteGrants(path string, sites []store.LegacyRuntimeSite) error {
	ids := make(map[string]bool)
	for _, s := range sites {
		if s.AccessID != "" {
			ids[s.AccessID] = true
		}
	}
	if len(ids) == 0 {
		return nil
	}
	access, err := webauth.LoadDelegatedAccess(path)
	if err != nil {
		return fmt.Errorf("loading delegated site access for removed sites: %w", err)
	}
	changed := false
	for username, user := range access.Users {
		for id := range ids {
			if _, ok := user.Sites[id]; ok {
				delete(user.Sites, id)
				user.Updated = time.Now().Unix()
				changed = true
			}
		}
		access.Users[username] = user
	}
	if changed {
		if err := webauth.SaveDelegatedAccess(path, access); err != nil {
			return fmt.Errorf("removing delegated access for removed sites: %w", err)
		}
	}
	return nil
}

func requireMpnServicesStopped() error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("cannot verify MCPN service state: %w", err)
	}
	for _, unit := range []string{"mpn-root.service", "mpn-web.service"} {
		out, err := exec.Command("systemctl", "show", "--property=ActiveState", "--value", unit).Output()
		if err != nil {
			return fmt.Errorf("checking %s: %w", unit, err)
		}
		switch strings.TrimSpace(string(out)) {
		case "inactive", "failed", "unknown":
			// Not serving requests, or this machine has no such unit.
		case "active", "activating", "deactivating", "reloading":
			return fmt.Errorf("stop mpn-root.service and mpn-web.service before applying database migrations")
		default:
			return fmt.Errorf("cannot verify %s state from systemd response %q", unit, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func runtimeRemovalPlan(sites []store.LegacyRuntimeSite) (installer.Plan, error) {
	for _, s := range sites {
		if !site.ValidName(s.Name) || !legacyUserName.MatchString(s.User) {
			return nil, fmt.Errorf("legacy site %q has an unsafe system user; no cleanup was attempted", s.Name)
		}
		if s.CustomVhost {
			return nil, fmt.Errorf("site %q has a custom vHost; preserve or remove it manually before migrating", s.Name)
		}
		cronPath := "/etc/cron.d/mpn-" + s.Name
		if info, err := os.Lstat(cronPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%s is a symlink; resolve it manually before migration", cronPath)
		} else if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if b, err := os.ReadFile(cronPath); err == nil {
			marker := "# Made by mpn for the website " + s.Name + ". Changes made here are overwritten; use the panel.\n"
			if !strings.HasPrefix(string(b), marker) {
				return nil, fmt.Errorf("%s is not identifiable as MCPN-generated; resolve it manually before migration", cronPath)
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		unitPath := "/etc/systemd/system/app-" + s.Name + ".service"
		if b, err := os.ReadFile(unitPath); err == nil {
			if !strings.Contains(string(b), "User="+s.User+"\n") || !strings.Contains(string(b), "Description="+map[string]string{"node": "Node.js", "python": "Python"}[s.Kind]+" app "+s.Name+"\n") {
				return nil, fmt.Errorf("%s is not an identifiable MCPN-generated unit; resolve it manually before migration", unitPath)
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		} else {
			if _, err := exec.LookPath("systemctl"); err != nil {
				return nil, fmt.Errorf("cannot verify whether app-%s.service is loaded: %w", s.Name, err)
			}
			for _, state := range []string{"is-active", "is-enabled"} {
				if err := exec.Command("systemctl", state, "--quiet", "app-"+s.Name+".service").Run(); err == nil {
					return nil, fmt.Errorf("app-%s.service is %s but its unit file is absent; resolve it manually before migration", s.Name, state)
				} else if _, ok := err.(*exec.ExitError); !ok {
					return nil, fmt.Errorf("checking app-%s.service state: %w", s.Name, err)
				}
			}
		}
		nginxPath := "/etc/nginx/sites-enabled/" + s.Name + ".conf"
		if b, err := os.ReadFile(nginxPath); err == nil {
			if !strings.Contains(string(b), "/var/log/mpn/sites/"+s.Name+"/") {
				return nil, fmt.Errorf("%s is not identifiable as an MCPN site config; resolve it manually before migration", nginxPath)
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return runtimeRemovalActions(sites), nil
}

func runtimeRemovalActions(sites []store.LegacyRuntimeSite) installer.Plan {
	var plan installer.Plan
	for _, s := range sites {
		unitPath := "/etc/systemd/system/app-" + s.Name + ".service"
		cronPath := "/etc/cron.d/mpn-" + s.Name
		nginxPath := "/etc/nginx/sites-enabled/" + s.Name + ".conf"
		socketPath := "/run/mpn-app/" + s.Name
		cronMarker := "# Made by mpn for the website " + s.Name + ". Changes made here are overwritten; use the panel."
		kind := map[string]string{"node": "Node.js", "python": "Python"}[s.Kind]
		script := fmt.Sprintf(`set -eu
unit=%q
file=%q
if [ -e "$file" ] || [ -L "$file" ]; then
  [ ! -L "$file" ] || { echo "refusing symlink unit $file" >&2; exit 1; }
  grep -Fqx %q "$file" || { echo "unit owner marker mismatch: $file" >&2; exit 1; }
  grep -Fqx %q "$file" || { echo "unit type marker mismatch: $file" >&2; exit 1; }
  if systemctl is-active --quiet "$unit"; then systemctl stop "$unit"; fi
  if systemctl is-enabled --quiet "$unit"; then systemctl disable "$unit"; fi
  rm -f -- "$file"
else
  if systemctl is-active --quiet "$unit" || systemctl is-enabled --quiet "$unit"; then
    echo "unit is active or enabled but its file is absent: $unit" >&2
    exit 1
  fi
fi`, "app-"+s.Name+".service", unitPath, "User="+s.User, "Description="+kind+" app "+s.Name)
		cronScript := fmt.Sprintf(`set -eu
file=%q
if [ -e "$file" ] || [ -L "$file" ]; then
  [ ! -L "$file" ] || { echo "refusing symlink cron file $file" >&2; exit 1; }
  grep -Fqx %q "${file}" || { echo "cron owner marker mismatch: $file" >&2; exit 1; }
  rm -f -- "$file"
fi`, cronPath, cronMarker)
		name := fmt.Sprintf("remove generated runtime configuration for %s (service %s, cron %s, nginx %s, socket %s; keep /home and databases)", s.Name, "app-"+s.Name+".service", cronPath, nginxPath, socketPath)
		plan = append(plan, installer.Step{Name: name, Actions: []installer.Action{installer.Run("sh", "-c", script), installer.Run("sh", "-c", cronScript), installer.Run("rm", "-f", nginxPath), installer.Run("rm", "-rf", socketPath)}})
	}
	if len(sites) > 0 {
		plan = append(plan, installer.Step{Name: "reload systemd and validate/reload nginx", Actions: []installer.Action{installer.Run("systemctl", "daemon-reload"), installer.Run("nginx", "-t"), installer.ReloadNginx()}})
	}
	return plan
}

var legacyUserName = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// panelRuntimeMigrationCommitted marks failures that occur after legacy site records have been removed. Callers must not
// restore an older binary after this point because it may not understand the committed schema.
var panelRuntimeMigrationCommitted bool

// panelMigrate brings a machine installed by an earlier version up to date with what a first install of this one writes. install.sh
// runs it after it has replaced the program. It does nothing on a machine that is up to date, and changes nothing without -apply.
func panelMigrate(st *store.Store, apply bool) {
	if apply {
		// The copy to off-site storage needs mpn-cloud beside mpn (from 1.2.0). Failing to fetch it stops only that copy, so it
		// is said and the rest goes on.
		if err := ensureMpnCloud(); err != nil {
			fmt.Fprintf(os.Stderr, "could not install %s: %v; the copy to off-site storage will not work until mpn update -apply runs\n", mpnCloudFile, err)
		}
	}
	nginxUpgrade(apply)
	notifyUnit(apply)
	unit, err := os.ReadFile(installer.WebUnitFile)
	if os.IsNotExist(err) {
		fmt.Println("the web panel is not installed on this machine; nothing to do")
		return
	}
	fail(err)
	sock, _ := os.ReadFile(installer.SocketFile)
	if migrated, ok := installer.MigrateWebUnit(string(unit)); ok && migrated == string(unit) && string(sock) == installer.SocketUnit() && panelConfOnSocket() && installer.SELinuxUpToDate() {
		fmt.Println("the panel already listens on its socket; nothing to do")
		return
	}
	acts, err := installer.MigrateActions(installer.MigrateInput{
		WebUnit: string(unit), PanelPort: panelPortOf(), PMA: fileExists(installer.PMAConfFile), EL: distro.Current() == distro.EL,
		Ports: installer.PanelPorts(panelPortOf()),
	})
	fail(err)
	plan := installer.Plan{{Name: "the panel on its socket", Actions: acts}}
	if !apply {
		fmt.Print(plan)
		fmt.Println("# checked only. Add -apply to change this machine.")
		return
	}
	requireLinuxRoot()
	if err := runPlanQuiet(plan); err != nil {
		fail(fmt.Errorf("moving the panel to its socket failed: %w\nthe panel's own files are in /etc/systemd/system and /etc/nginx/conf.d; run mpn panel migrate -apply again once the cause is fixed", err))
	}
	// The blocks of the panel's name on port 443 and the pages of the database browser follow the same way.
	fail(syncFront(st))
	fmt.Println("the panel now listens on its socket, " + installer.PanelSocket)
}

// nginxUpgrade moves nginx from nginx.org to the version this release pins (installer.NginxVersion), with the modules the machine
// loads now built for that version. A machine already there, or one whose nginx comes from its distribution, is left alone.
func nginxUpgrade(apply bool) {
	raw, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return
	}
	t, err := osinfo.Parse(string(raw))
	if err != nil {
		return
	}
	v, _ := exec.Command("nginx", "-v").CombinedOutput() // "nginx -v" prints on standard error
	running := installer.NginxVersionOf(string(v))
	distroBuild := installer.DistroNginx(string(v))
	if !installer.NginxUpgradeNeeded(t, running, distroBuild) {
		return
	}
	var mods []string
	for _, kind := range []string{installer.ModPageSpeed, installer.ModModSecurity} {
		if installer.ModuleLoaded(kind) {
			mods = append(mods, kind)
		}
	}
	acts, err := installer.NginxUpgradeActions(installer.NginxUpgradeInput{Target: t, Running: running, Distro: distroBuild, Modules: mods})
	fail(err)
	plan := installer.Plan{{Name: "nginx " + running + " to " + installer.NginxVersion, Actions: acts}}
	if !apply {
		fmt.Print(plan)
		fmt.Println("# checked only. Add -apply to change this machine.")
		return
	}
	requireLinuxRoot()
	if err := runPlanQuiet(plan); err != nil {
		fail(fmt.Errorf("moving nginx to %s failed: %w\nnginx %s and its modules were put back; the files before the change are in /var/backups/mpn-nginx-upgrade/old", installer.NginxVersion, err, running))
	}
}

// notifyUnit writes the unit of the Telegram alerts (from 1.3.0) on a machine with the panel, and starts it. A unit that is already as
// this version writes it is left alone, so an owner who stopped the service keeps it stopped.
func notifyUnit(apply bool) {
	if !fileExists(installer.WebUnitFile) {
		return
	}
	if b, err := os.ReadFile(installer.NotifyUnitFile); err == nil && string(b) == installer.NotifyService {
		return
	}
	plan := installer.Plan{{Name: "the Telegram alerts service", Actions: []installer.Action{
		installer.WriteFile(installer.NotifyUnitFile, installer.NotifyService, 0o644),
		installer.Run("systemctl", "daemon-reload"),
		installer.Run("systemctl", "enable", "mpn-notify"),
		installer.Run("systemctl", "restart", "mpn-notify"),
	}}}
	if !apply {
		fmt.Print(plan)
		return
	}
	requireLinuxRoot()
	if err := runPlanQuiet(plan); err != nil {
		fmt.Fprintf(os.Stderr, "the Telegram alerts service could not be installed: %v\n", err)
	}
}

// panelConfOnSocket says whether the panel's nginx file points at the socket.
func panelConfOnSocket() bool {
	b, err := os.ReadFile(installer.PanelConfFile)
	return err == nil && strings.Contains(string(b), "proxy_pass "+installer.PanelUpstream+";")
}
