package installer

import (
	"strings"
	"testing"
)

func panelOpts() Options {
	o := opts("", 1024)
	o.SelfPath, o.Panel, o.PanelPort, o.PanelHosts = "/root/mpn", true, 2610, []string{"panel.example.com"}
	return o
}

func TestPanelStepOrderAndContents(t *testing.T) {
	p, err := Build(panelOpts())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(p), "base packages,journald limit,nginx,nginx pagespeed,mise,renewal and backup timers,web panel"; got != want {
		t.Fatalf("steps=%s want %s", got, want)
	}
	out := p.String()
	// The socket is made and started before nginx is pointed at it.
	order := []string{"useradd --system", "install -d -o mpn-web -g mpn-web -m 0750 /var/lib/mpn-web", "groupadd --system mpn-front",
		"write /etc/systemd/system/mpn-root.service", "write /etc/systemd/system/mpn-web.socket", "write /etc/systemd/system/mpn-web.service",
		"d=/etc/mpn/certs/_panel", "run   systemctl enable mpn-root mpn-web.socket mpn-web",
		"run   systemctl restart mpn-root", "rmdir /run/mpn-web", "run   systemctl restart mpn-web",
		"write /etc/nginx/conf.d/01-mpn-panel.conf", "run   nginx -t", "/run/mpn-nginx-restart"}
	last := -1
	for _, w := range order {
		i := strings.Index(out[strings.Index(out, "web panel"):], w)
		if i < 0 || i < last {
			t.Fatalf("%q missing or out of order in:\n%s", w, out)
		}
		last = i
	}
	if strings.Contains(out, "web passwd") {
		t.Errorf("the account is made by the first-run page, not by the installer:\n%s", out)
	}
}

// The socket is the only way to the panel. Its folder lets only root and the group in, so no other account can connect, and no other
// program can put a socket in its place while the panel restarts (the socket belongs to systemd and stays).
func TestThePanelSocketIsOpenOnlyToItsGroup(t *testing.T) {
	// The folder is made root:root by systemd, so the web server must be able to pass through it (seen on Debian 12: 0750 gave 502).
	for _, want := range []string{"ListenStream=/run/mpn-web/web.sock", "SocketUser=root", "SocketGroup=mpn-front", "SocketMode=0660", "DirectoryMode=0755",
		"[Install]\nWantedBy=sockets.target"} {
		if !strings.Contains(panelSocketUnit, want) {
			t.Errorf("missing %q in:\n%s", want, panelSocketUnit)
		}
	}
	s := frontGroupScript("www-data")
	if !strings.Contains(s, "usermod -aG mpn-front www-data") || !strings.Contains(s, "touch /run/mpn-nginx-restart") {
		t.Errorf("the web server must join the group, and nginx be restarted once to see it:\n%s", s)
	}
	if !strings.Contains(pmaUserScript, "usermod -aG mpn-front mpn-pma") {
		t.Error("the database browser's sign-on page needs the socket too")
	}
}

func TestPanelCertificateScriptIsSelfSignedAndPrivate(t *testing.T) {
	for _, want := range []string{"openssl req -x509", "chmod 600", "/etc/mpn/certs/_panel"} {
		if !strings.Contains(panelCertScript, want) {
			t.Errorf("missing %q in:\n%s", want, panelCertScript)
		}
	}
}

func TestPanelAccountIsOnlyTakenOverWhenMpnMadeIt(t *testing.T) {
	if !strings.Contains(webUserScript, `= 'mpn web panel' ] || {`) || !strings.Contains(webUserScript, "exit 1") {
		t.Errorf("an existing mpn-web account must carry mpn's mark:\n%s", webUserScript)
	}
}

func TestPanelServiceIsConfinedAndListensOnLoopbackOnly(t *testing.T) {
	s := webService([]string{"panel.example.com"}, 0)
	for _, want := range []string{"User=mpn-web", "NoNewPrivileges=yes", "CapabilityBoundingSet=\n", "ProtectSystem=strict", "ProtectHome=yes",
		"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6", "MemoryMax=96M",
		"ExecStart=/usr/local/bin/mpn web -listen systemd -secure -trust-proxy -host panel.example.com", "Requires=mpn-web.socket"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "8780") {
		t.Error("the panel must not listen on a TCP port that any program of the machine could take")
	}
	if strings.Contains(s, "0.0.0.0") || strings.Contains(s, "User=root") {
		t.Error("the panel must not listen publicly or run as root")
	}
	if !strings.Contains(rootService, "ExecStart=/usr/local/bin/mpn root") || !strings.Contains(rootService, "RuntimeDirectory=mpn") {
		t.Errorf("root helper unit:\n%s", rootService)
	}
	// Without an [Install] section "systemctl enable" does nothing, and the panel would not start after a reboot.
	for name, unit := range map[string]string{"root": rootService, "web": s} {
		if !strings.Contains(unit, "[Install]\nWantedBy=multi-user.target") {
			t.Errorf("mpn-%s.service must be enableable", name)
		}
	}
	if strings.Contains(rootService, "UMask") {
		t.Error("a umask on the helper would change the permissions of every site it creates")
	}
}

func TestPanelNginxTerminatesTLSAndPassesTheRightHeaders(t *testing.T) {
	c := panelNginx(2610)
	for _, want := range []string{"listen 2610 ssl http2;", "listen [::]:2610 ssl http2;", "proxy_pass http://unix:/run/mpn-web/web.sock:;",
		"proxy_set_header Host $http_host;", "proxy_set_header X-Real-IP $remote_addr;", "/etc/mpn/certs/_panel/fullchain.pem", "client_max_body_size 1m;",
		"gzip_types text/css text/javascript application/javascript;"} {
		if !strings.Contains(c, want) {
			t.Errorf("missing %q in:\n%s", want, c)
		}
	}
}

func TestPanelNginxLetsLargeUploadsThroughOnlyWhereTheyBelong(t *testing.T) {
	c := panelNginx(2610)
	up := strings.Index(c, "location ~ ^/sites/[^/]+/files/upload$ {")
	save := strings.Index(c, "location ~ ^/sites/[^/]+/files/save$ {")
	root := strings.Index(c, "location / {")
	if up < 0 || save < up || root < save {
		t.Fatalf("locations missing or out of order:\n%s", c)
	}
	if blk := c[up:save]; !strings.Contains(blk, "client_max_body_size 1100m;") || !strings.Contains(blk, "proxy_request_buffering off;") || !strings.Contains(blk, "proxy_pass "+PanelUpstream+";") {
		t.Errorf("upload location:\n%s", blk)
	}
	if blk := c[save:root]; !strings.Contains(blk, "client_max_body_size 8m;") || strings.Contains(blk, "1100m") {
		t.Errorf("save location:\n%s", blk)
	}
	// every place that hands a request to the panel must pass the same headers, or the Host check and the client address break
	if n := strings.Count(c, "proxy_set_header Host $http_host;"); n != 4 || strings.Count(c, "proxy_set_header X-Real-IP $remote_addr;") != 4 {
		t.Errorf("the headers must be set in all four locations (setup, upload, save, the rest):\n%s", c)
	}
	if blk := c[root:]; strings.Contains(blk, "1100m") || strings.Contains(blk, "request_buffering") {
		t.Errorf("the large limit leaked into the general location:\n%s", blk)
	}
}

func TestPanelRejectsBadSettings(t *testing.T) {
	for name, mutate := range map[string]func(*Options){
		"no binary":     func(o *Options) { o.SelfPath = "" },
		"low port":      func(o *Options) { o.PanelPort = 80 },
		"backend port":  func(o *Options) { o.PanelPort = PanelBackendPort },
		"huge port":     func(o *Options) { o.PanelPort = 70000 },
		"host injected": func(o *Options) { o.PanelHosts = []string{"a.com; } server {"} },
		"upper host":    func(o *Options) { o.PanelHosts = []string{"Panel.Example.com"} },
	} {
		o := panelOpts()
		mutate(&o)
		if _, err := Build(o); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	o := panelOpts()
	o.Panel = false
	p, _ := Build(o)
	if strings.Contains(names(p), "web panel") {
		t.Error("the panel must be opt-in")
	}
	o = panelOpts()
	o.PanelPort = 0
	if p, err := Build(o); err != nil || !strings.Contains(p.String(), "01-mpn-panel.conf") {
		t.Errorf("port 0 must mean the default: %v", err)
	}
}

func TestPanelKeepsSshFromPenalisingItsOwnAddress(t *testing.T) {
	p, err := Build(panelOpts())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range p[len(p)-1].Actions {
		if len(a.Args) == 3 && a.Args[2] == sshdExemptScript {
			found = true
		}
	}
	if !found {
		t.Fatal("the panel step does not run the sshd exemption script")
	}
	for _, want := range []string{"PerSourcePenaltyExemptList 127.0.0.1/32,::1/128", `"$S" -T`, "persourcepenaltyexemptlist", `"$S" -t`, `rm -f "$f"`, "exit 0"} {
		if !strings.Contains(sshdExemptScript, want) {
			t.Errorf("the script lacks %q", want)
		}
	}
	// The check that sshd knows the option must come before the file is written, and sshd must be validated before reload.
	i, j, k := strings.Index(sshdExemptScript, "grep -qi"), strings.Index(sshdExemptScript, `> "$f"`), strings.Index(sshdExemptScript, "reload")
	v := strings.Index(sshdExemptScript, `"$S" -t`)
	if !(i < j && j < v && v < k) {
		t.Errorf("order wrong: detect=%d write=%d validate=%d reload=%d", i, j, v, k)
	}
}

func TestPanelNginxPassesTheWebSocketUpgrade(t *testing.T) {
	c := panelNginx(2610)
	for _, want := range []string{"proxy_http_version 1.1;", "proxy_set_header Upgrade $http_upgrade;", "proxy_set_header Connection $connection_upgrade;"} {
		if !strings.Contains(c, want) {
			t.Errorf("missing %q in:\n%s", want, c)
		}
	}
	// $connection_upgrade comes from the map that the main nginx.conf declares.
	if !strings.Contains(nginxConf(opts("", 1024).Profile), "map $http_upgrade $connection_upgrade") {
		t.Error("nginx.conf must declare the $connection_upgrade map the panel's server block uses")
	}
}
