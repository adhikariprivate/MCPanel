// Package installer builds the ordered list of changes needed to prepare a fresh server.
// It only describes the work (a Plan). Nothing here touches the machine.
package installer

import (
	_ "embed"
	"fmt"
	"runtime"
	"slices"
	"strings"

	"mpn/internal/aptsrc"
	"mpn/internal/distro"
	"mpn/internal/osinfo"
	"mpn/internal/site"
	"mpn/internal/tune"
)

// Action is either a file write (Path set) or a command (Args set).
type Action struct {
	Path, Content string
	Mode          uint32
	Args          []string
}

func WriteFile(path, content string, mode uint32) Action {
	return Action{Path: path, Content: content, Mode: mode}
}

func Run(args ...string) Action { return Action{Args: args} }

// AptInstall installs packages without questions and waits for the dpkg lock; other commands of mpn use it for optional parts.
func AptInstall(pkgs ...string) Action { return aptInstall(pkgs...) }

// nginxReloadScript reloads nginx and returns only when the old workers have stopped taking new connections.
// "systemctl reload nginx" returns as soon as the signal is sent. For about a tenth of a second after that the old
// workers still accept connections, and they close those when they shut down; a request that arrives then is cut off
// (measured on nginx 1.18: every request sent within 50 ms of the reload failed, none after 200 ms). The page a person is
// sent to right after "apply" arrives exactly then. A worker that is shutting down changes its process title, and the
// workers are the children of the master, which also keeps this script from finding itself. Waiting is limited to 5
// seconds, and a plan never fails because of it.
const nginxReloadScript = `old=; [ -r /run/nginx.pid ] && old=$(ps -o pid= --ppid "$(cat /run/nginx.pid)" 2>/dev/null)
systemctl reload nginx || exit 1
i=0
while [ "$i" -lt 50 ]; do
  busy=0
  for p in $old; do
    c=$(tr '\0' ' ' < "/proc/$p/cmdline" 2>/dev/null)
    case "$c" in ''|*'shutting down'*) ;; *) busy=1 ;; esac
  done
  [ "$busy" = 0 ] && exit 0
  i=$((i+1)); sleep 0.1
done
exit 0`

// The workers are found as the children of the master by its process number, never by name (see
// TestInstallerDoesNotWaitForPermanentDaemons).
// ReloadNginx is the step every plan uses to make nginx read its configuration again.
func ReloadNginx() Action { return Run("sh", "-c", nginxReloadScript) }

// The package lists are read again with retries (distro.RefreshScript). A freshly created server often runs unattended-upgrades
// or cloud-init package work for the first minutes, and "update" then fails at once on the lists lock. Waiting for a process by
// name does not work: unattended-upgrade-shutdown is a permanent daemon on Debian and Ubuntu.

// pagespdAptScript adds the We-Amp mod_pagespeed apt repository (packages.modpagespeed.com) so that
// nginx-module-pagespeed can be installed. The repo ships builds matched to the distro's nginx package.
const pagespdAptScript = `set -eu
want=DF00E296BE9141CEA5411346F50D6054F10712A0
keyring=/usr/share/keyrings/modpagespeed-archive-keyring.gpg
tmp=$(mktemp)
trap 'rm -f "$tmp" "$keyring.tmp"' EXIT
curl -fsSL --proto '=https' https://packages.modpagespeed.com/pubkey.gpg -o "$tmp"
got=$(gpg --batch --with-colons --show-keys "$tmp" | awk -F: '$1=="fpr"{print $10}' | sort -u)
[ "$got" = "$want" ] || { echo "modpagespeed signing key has an unexpected fingerprint: $got" >&2; exit 1; }
gpg --dearmor < "$tmp" > "$keyring.tmp"
mv "$keyring.tmp" "$keyring"
. /etc/os-release
echo "deb [arch=$(dpkg --print-architecture) signed-by=$keyring] https://packages.modpagespeed.com/apt $VERSION_CODENAME main" > /etc/apt/sources.list.d/modpagespeed.list`

// aptUpdate retries on failure; aptInstall waits up to 5 minutes for the dpkg lock instead of failing at once. Both use the
// package manager of the system (apt on Debian and Ubuntu, dnf on the RHEL family); the names stay for the callers.
func aptUpdate() Action { return Run("sh", "-c", distro.RefreshScript()) }

func aptInstall(pkgs ...string) Action { return Run(distro.InstallCommand(pkgs...)...) }

type Step struct {
	Name    string
	Actions []Action
}

type Plan []Step

type Options struct {
	Target        osinfo.Target
	Profile       tune.Profile
	CurrentSwapMB int
	Engine        string   // "mariadb", "mysql", "postgresql", or "" to skip the database
	PostgreSQL    bool     // a PostgreSQL server is already installed, so PHP gets its pgsql driver
	DBSeries      string   // series of the chosen engine, such as "11.8" or "8.4"; empty means the engine's default
	PHPVersions   []string // for example "8.4"; each gets php-fpm and a shared opcache file, and its service is left stopped
	NodeVersions  []string // major versions such as "22", installed under /opt/node
	SelfPath      string   // path of this binary; when set it is installed and the renewal timer is created
	Panel         bool     // install the web panel; needs SelfPath
	PanelPort     int      // public HTTPS port of the panel, 2610 when 0
	PMAPort       int      // port of the database browser; the one after the panel's when 0
	PanelHosts    []string // names the panel is reached by; IP addresses are always accepted
	PMA           bool     // install the database browser (phpMyAdmin) on the port after the panel's; needs Panel and Engine
}

// Build returns the plan. It fails on an unknown engine or MySQL series.
func Build(o Options) (Plan, error) {
	// cron runs the jobs of the Cron tab; minimal images (Debian 13 on some providers) come without it.
	base := Step{"base packages", []Action{aptUpdate(), aptInstall("ca-certificates", "curl", "xz-utils", "openssl", "logrotate", "gpgv", distro.CronPackage()), Run("systemctl", "enable", "--now", distro.CronService())}}
	nginxActs := nginxInstallActions(o.Target)
	if o.Target.EL() {
		base = Step{"base packages", []Action{
			Run("sh", "-c", distro.RefreshScript()),
			elEPEL(),
			Run(distro.InstallCommand(elBasePackages()...)...),
			Run("systemctl", "enable", "--now", distro.CronService()),
			elFirewallAction(),
		}}
		nginxActs = elNginxInstallActions()
	}
	plan := Plan{base}
	// The kernel reports a swap file a little smaller than its size, so small gaps are ignored.
	if need := o.Profile.SwapMB - o.CurrentSwapMB; need >= 64 {
		plan = append(plan, Step{"swap", []Action{
			Run("fallocate", "-l", fmt.Sprintf("%dM", need), "/swapfile"),
			Run("chmod", "600", "/swapfile"),
			Run("mkswap", "/swapfile"),
			Run("swapon", "/swapfile"),
			Run("sh", "-c", "grep -q '^/swapfile' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab"),
		}})
	}
	psStep, err := pageSpeedStep(o.Target)
	if err != nil {
		return nil, err
	}
	plan = append(plan,
		Step{"journald limit", []Action{
			WriteFile("/etc/systemd/journald.conf.d/mpn.conf", fmt.Sprintf("[Journal]\nSystemMaxUse=%dM\n", o.Profile.JournalMaxMB), 0o644),
			Run("systemctl", "restart", "systemd-journald"),
		}},
		Step{"nginx", slices.Concat(nginxActs, []Action{
			Run("systemctl", "enable", "--now", "nginx"),
			WriteFile("/etc/nginx/nginx.conf", nginxConf(o.Profile), 0o644),
			// Debian and Ubuntu enable a default site that would clash with the catch-all below.
			Run("rm", "-f", "/etc/nginx/sites-enabled/default"),
			Run("sh", "-c", defaultCertScript),
			Run("install", "-d", "-m", "0755", "-o", "root", "-g", "root", defaultPageDir),
			WriteFile(defaultPageDir+"/index.html", defaultPage, 0o644),
			WriteFile("/etc/nginx/mpn-default-page.conf", defaultPageConf, 0o644),
			WriteFile("/etc/nginx/conf.d/00-default.conf", defaultServer, 0o644),
			Run("install", "-d", "-m", "0755", "/var/log/mpn/sites"),
			WriteFile("/etc/logrotate.d/mpn-sites", siteLogRotation, 0o644),
			Run("logrotate", "-d", "/etc/logrotate.d/mpn-sites"), // a syntax error makes this fail before anything reloads
			Run("nginx", "-t"),
			ReloadNginx(),
		})},
		psStep,
		Step{"mise", miseInstallStep()},
	)
	if o.Target.EL() {
		plan = append(plan, SELinuxStep(elPanelPorts(o)))
	}
	if o.SelfPath != "" {
		plan = append(plan, serviceStep(o.SelfPath))
	}
	if o.Panel {
		s, err := panelStep(o)
		if err != nil {
			return nil, err
		}
		plan = append(plan, s)
	}
	if len(o.PHPVersions) > 0 {
		s, err := phpStep(o)
		if err != nil {
			return nil, err
		}
		plan = append(plan, s)
	}
	for _, v := range o.NodeVersions {
		s, err := nodeStep(v)
		if err != nil {
			return nil, err
		}
		plan = append(plan, s)
	}
	if o.Engine != "" {
		s, err := database(o)
		if err != nil {
			return nil, err
		}
		plan = append(plan, s)
	}
	if o.PMA && o.Engine != "postgresql" {
		s, err := pmaStep(o)
		if err != nil {
			return nil, err
		}
		plan = append(plan, s)
	}
	return plan, nil
}

// miseInstallStep installs the pinned upstream executable after checking the published archive's SHA-256.
func miseInstallStep() []Action {
	return miseInstallStepForArch(runtime.GOARCH)
}

func miseInstallStepForArch(arch string) []Action {
	name, sum, err := miseAsset(arch)
	if err != nil {
		return []Action{Run("sh", "-c", "echo 'mise supports Linux amd64 and arm64 only' >&2; exit 1")}
	}
	url := "https://github.com/jdx/mise/releases/download/v2026.11.13/" + name
	script := `set -eu
if [ -x /usr/local/bin/mise ] && /usr/local/bin/mise --version 2>/dev/null | grep -Fq '2026.11.13'; then exit 0; fi
d=$(mktemp -d); trap 'rm -rf "$d"' EXIT
curl -fsSL --retry 3 --proto '=https' '` + url + `' -o "$d/mise.tar.gz"
printf '%s  %s\n' '` + sum + `' "$d/mise.tar.gz" | sha256sum -c -
tar -xzf "$d/mise.tar.gz" -C "$d"
test -x "$d/mise/bin/mise"
install -m 0755 "$d/mise/bin/mise" /usr/local/bin/mise`
	return []Action{Run("sh", "-c", script)}
}

func miseAsset(arch string) (string, string, error) {
	name, sum := "", ""
	switch arch {
	case "amd64":
		name, sum = "mise-v2026.11.13-linux-x64.tar.gz", "22a2c32160a152d170261586dc124644d2ac2ab36c0afb48ef2b5d275c485a60"
	case "arm64":
		name, sum = "mise-v2026.11.13-linux-arm64.tar.gz", "31361f75c9b4ad38bfd4f8ca1a9e1cd8e8f40c5cdba432cc552adb48ac21a683"
	default:
		return "", "", fmt.Errorf("unsupported mise architecture %q", arch)
	}
	return name, sum, nil
}

// EnsureMise returns the pinned, checksum-verifying installer action for site provisioning on upgraded servers.
func EnsureMise() Action { return miseInstallStep()[0] }

// phpStep adds the Sury repo and installs each PHP version. The FPM service is stopped afterwards:
// a version only runs once a site uses it, which keeps idle masters out of RAM.
func phpStep(o Options) (Step, error) {
	if o.Target.EL() {
		return elPHPStep(o)
	}
	src := aptsrc.PHP(o.Target)
	acts := []Action{
		Run("curl", "-fsSL", "--retry", "3", "-o", src.KeyFile, src.KeyURL),
		WriteFile(src.Path(), src.Content, 0o644),
		aptUpdate(),
	}
	for _, v := range o.PHPVersions {
		if !slices.Contains(site.PHPVersions, v) {
			return Step{}, fmt.Errorf("php version %q is not offered", v)
		}
		exts := []string{"fpm", "cli", "mysql", "mbstring", "xml", "curl", "zip", "gd"}
		if o.PostgreSQL || o.Engine == "postgresql" {
			exts = append(exts, "pgsql")
		}
		if v < "8.0" {
			exts = append(exts, "opcache") // built in from 8.0 on
		}
		// EL: PHP 7.0 to 7.3 have no packages (distro.OwnPHP); their tarballs from mpn-packages are added in the RHEL work.
		pkgs := distro.PHPPackages(v, exts)
		acts = append(acts,
			aptInstall(pkgs...),
			WriteFile(distro.PHPConfDir(v)+"99-mpn.ini", o.Profile.OpcacheINI(), 0o644),
			Run("sh", "-c", stopUnusedFPM(v)),
		)
	}
	return Step{"php " + strings.Join(o.PHPVersions, ","), acts}, nil
}

// stopUnusedFPM stops a PHP-FPM service that has no site pool. Any pool other than the package's own www.conf means
// a site uses this version, and stopping the service would take that site down every time the plan is applied again.
func stopUnusedFPM(v string) string {
	return fmt.Sprintf("if ls %[1]s*.conf 2>/dev/null | grep -qv '/www\\.conf$'; then echo '%[2]s has a site pool, left running'; else systemctl disable --now %[2]s; fi", distro.PHPPoolDir(v), distro.PHPService(v))
}

// nodeReleaseKeys are the keys of the people who sign Node.js releases, active and retired, from nodejs/release-keys at commit
// 481637f813e912c4aa3622d7964ab426c97b8e8d (29 keys, each file name checked against the fingerprint of its key), exported as one
// binary keyring for gpgv. They are built into mpn so that neither nodejs.org nor a copy of it can choose the keys.
//
//go:embed nodekeys/node-release-keys.gpg
var nodeReleaseKeys []byte

// NodeKeyringFile is where the step writes those keys for gpgv.
const NodeKeyringFile = "/etc/mpn/node-release-keys.gpg"

// nodeScript installs the newest release of one major version from nodejs.org into /opt/node/<release>
// and points /opt/node/<major> at it. SHASUMS256.txt must carry a good signature of a Node.js releaser
// (gpgv against the keys above) before any line of it is believed, and the tarball must match its line.
const nodeScript = `set -eu
command -v gpgv >/dev/null || { echo "gpgv is needed to check Node.js (the base packages step installs it)" >&2; exit 1; }
major=@MAJOR@
case $(uname -m) in x86_64) a=x64;; aarch64) a=arm64;; *) echo "unsupported CPU" >&2; exit 1;; esac
ver=$(curl -fsSL https://nodejs.org/dist/index.json | grep -o "\"version\":\"v$major\.[0-9]*\.[0-9]*\"" | head -1 | cut -d'"' -f4)
[ -n "$ver" ] || { echo "no Node.js release for $major" >&2; exit 1; }
dir=/opt/node/$ver
if [ ! -x "$dir/bin/node" ]; then
  tmp=$(mktemp -d)
  f=node-$ver-linux-$a.tar.xz
  curl -fsSL -o "$tmp/$f" "https://nodejs.org/dist/$ver/$f"
  curl -fsSL -o "$tmp/SHASUMS256.txt" "https://nodejs.org/dist/$ver/SHASUMS256.txt"
  curl -fsSL -o "$tmp/SHASUMS256.txt.sig" "https://nodejs.org/dist/$ver/SHASUMS256.txt.sig"
  gpgv --keyring ` + NodeKeyringFile + ` "$tmp/SHASUMS256.txt.sig" "$tmp/SHASUMS256.txt" 2>/dev/null || { echo "SHASUMS256.txt of Node.js $ver is not signed by a Node.js releaser; nothing was installed" >&2; rm -rf "$tmp"; exit 1; }
  (cd "$tmp" && grep " $f\$" SHASUMS256.txt | sha256sum -c -)
  mkdir -p /opt/node
  tar -xJf "$tmp/$f" -C /opt/node
  mv "/opt/node/node-$ver-linux-$a" "$dir"
  rm -rf "$tmp"
fi
ln -sfn "$dir" "/opt/node/$major"
`

func nodeStep(major string) (Step, error) {
	if !slices.Contains(site.NodeVersions, major) {
		return Step{}, fmt.Errorf("node version %q is not offered", major)
	}
	// gpgv is in the base packages from 1.2.0; a machine installed before gets it here, the first time it adds a Node.js version.
	return Step{"node " + major, []Action{
		Run("sh", "-c", "command -v gpgv >/dev/null || "+strings.Join(distro.InstallCommand(distro.GPGVPackage()), " ")),
		WriteFile(NodeKeyringFile, string(nodeReleaseKeys), 0o644),
		Run("sh", "-c", strings.Replace(nodeScript, "@MAJOR@", major, 1)),
	}}, nil
}

func hasPHPBefore74(versions []string) bool {
	for _, v := range versions {
		if v < "7.4" {
			return true
		}
	}
	return false
}

// postgresqlDriverActions adds the PHP pgsql/PDO_pgsql package to installed runtimes when PostgreSQL is added.
// The custom EL PHP 7.0–7.3 builds have no separately packaged pgsql module; those runtimes are not supported for PostgreSQL.
func postgresqlDriverActions(o Options) []Action {
	var acts []Action
	for _, v := range o.PHPVersions {
		if distro.OwnPHP(v) {
			continue
		}
		pkgs := distro.PHPPackages(v, []string{"pgsql"})
		if len(pkgs) == 0 {
			continue
		}
		acts = append(acts,
			Run(distro.InstallCommand(pkgs...)...),
			Run("systemctl", "try-restart", distro.PHPService(v)+".service"),
		)
	}
	return acts
}

// database adds the repo, installs the server and writes the tuned config. The config file paths
// are the usual ones for Debian and Ubuntu packages and have not been checked on a real install yet.
func database(o Options) (Step, error) {
	series := o.DBSeries
	if series == "" {
		series = osinfo.DefaultSeries(o.Engine)
	}
	if o.Target.EL() {
		return elDatabaseStep(o, series)
	}
	if o.Engine == "postgresql" {
		if !o.Target.HasSeries("postgresql", series) {
			return Step{}, fmt.Errorf("PostgreSQL %s is not offered on %s %s; choose one of: %s", series, o.Target.ID, o.Target.Version, strings.Join(o.Target.Series("postgresql"), ", "))
		}
		src, err := aptsrc.PostgreSQL(o.Target, series)
		if err != nil {
			return Step{}, err
		}
		keyScript := `set -eu
t=$(mktemp); trap 'rm -f "$t"' EXIT
curl -fsSL --retry 3 --proto '=https' '` + src.KeyURL + `' -o "$t"
got=$(gpg --batch --with-colons --show-keys "$t" | awk -F: '$1=="fpr"{print $10}' | sort -u)
[ "$got" = '` + src.KeyFingerprint + `' ] || { echo "PostgreSQL signing key fingerprint mismatch: $got" >&2; exit 1; }
install -m 0644 "$t" ` + src.KeyFile
		acts := []Action{
			Run("sh", "-c", keyScript),
			WriteFile(src.Path(), src.Content, 0o644), aptUpdate(), aptInstall("postgresql-"+series, "postgresql-contrib-"+series),
		}
		acts = append(acts, postgresqlDriverActions(o)...)
		return Step{"database postgresql", acts}, nil
	}
	var src aptsrc.Source
	var err error
	switch o.Engine {
	case "mariadb":
		src, err = aptsrc.MariaDB(o.Target, series)
	case "mysql":
		src, err = aptsrc.MySQL(o.Target, series)
	case "postgresql":
		return Step{}, fmt.Errorf("PostgreSQL was already handled above")
	default:
		return Step{}, fmt.Errorf("unknown engine %q (want mariadb or mysql)", o.Engine)
	}
	if err != nil {
		return Step{}, err
	}
	pkg, svc, conf := distro.DBPackage(o.Engine), distro.DBService(o.Engine), distro.DBConfFile(o.Engine)
	cnf, err := o.Profile.DBConf(o.Engine)
	if err != nil {
		return Step{}, err
	}
	if o.Engine == "mysql" && hasPHPBefore74(o.PHPVersions) {
		cnf += tune.LegacyAuthConf(series)
	}
	return Step{"database " + o.Engine, []Action{
		Run("curl", "-fsSL", "--retry", "3", "-o", src.KeyFile, src.KeyURL),
		WriteFile(src.Path(), src.Content, 0o644),
		aptUpdate(),
		aptInstall(pkg),
		WriteFile(conf, cnf, 0o644),
		Run("systemctl", "restart", svc),
	}}, nil
}

// siteLogRotation rotates every site's logs daily and keeps 14 days (a log over 200MB is rotated at the next run
// even if it is younger than a day). nginx is told to reload (HUP), not to reopen its logs (USR1): on USR1 the master
// reopens the files as root but the workers reopen them again by path as www-data, which cannot enter the root-owned
// 0750 site directories, so they kept writing to the rotated file (seen on Ubuntu 22.04 and Debian 13). On a reload the
// master opens every log as root and the new workers inherit the descriptors. New files are 0644 because the
// per-site directory, not the file, decides who can read them. logrotate refuses directories writable by anyone but
// root, which is one more reason the log directories belong to root. The rotated name carries the epoch second
// (dateformat -%Y%m%d-%s): with the plain date, a second rotation on the same day (maxsize can trigger one) finds the
// name taken, skips the rotation, and the log keeps growing (seen with "destination ... already exists").
const siteLogRotation = `/var/log/mpn/sites/*/*.log {
    daily
    rotate 14
    maxsize 200M
    missingok
    notifempty
    compress
    delaycompress
    dateext
    dateformat -%Y%m%d-%s
    sharedscripts
    create 0644 root root
    postrotate
        [ ! -f /run/nginx.pid ] || kill -HUP "$(cat /run/nginx.pid)"
    endscript
}
`

// defaultPageDir holds the page shown for the server's IP address and for any host name no site has. It belongs to
// root and holds nothing else, so the catch-all server can read no file of a site.
const defaultPageDir = "/usr/local/share/mpn/default"

//go:embed pages/default.html
var defaultPage string

// defaultServer answers requests for the IP address and for unknown host names with one static page, on both ports,
// instead of closing the connection (the browser then said "didn't send any data", which looks like a broken server).
// Every path gets the same page, and nothing of a site or of the panel is named in it. Port 443 needs a certificate to
// complete the handshake, so a self-signed one is generated.
const defaultServer = `server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;
    include /etc/nginx/mpn-default-page.conf;
}

server {
    listen 443 ssl default_server;
    listen [::]:443 ssl default_server;
    server_name _;
    ssl_certificate /etc/mpn/certs/_default/fullchain.pem;
    ssl_certificate_key /etc/mpn/certs/_default/privkey.pem;
    include /etc/nginx/mpn-default-page.conf;
}
`

// defaultPageConf is shared by both servers of defaultServer. The page has its own inline style and script and loads
// nothing, so the policy allows only those.
const defaultPageConf = `root ` + defaultPageDir + `;
access_log off;
add_header X-Content-Type-Options nosniff always;
add_header Content-Security-Policy "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; frame-ancestors 'none'" always;
location / {
    limit_except GET { deny all; }
    try_files /index.html =404;
}
`

// defaultCertScript creates the self-signed certificate for the catch-all server once. Site names cannot start
// with an underscore, so "_default" never clashes with a site.
const defaultCertScript = `d=/etc/mpn/certs/_default
[ -s "$d/fullchain.pem" ] || { mkdir -p "$d" && openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 3650 -subj /CN=default -keyout "$d/privkey.pem" -out "$d/fullchain.pem" && chmod 600 "$d/privkey.pem"; }`

const (
	mpnBinary    = "/usr/local/bin/mpn"
	renewService = "[Unit]\nDescription=Renew Let's Encrypt certificates\n\n[Service]\nType=oneshot\nExecStart=" + mpnBinary + " site ssl-renew -apply\n"
	renewTimer   = "[Unit]\nDescription=Renew Let's Encrypt certificates daily\n\n[Timer]\nOnCalendar=daily\nRandomizedDelaySec=1h\nPersistent=true\n\n[Install]\nWantedBy=timers.target\n"
)

// serviceStep installs this binary and the timer that renews certificates. The renewal only touches sites
// that have HTTPS enabled and whose certificate expires soon.
func serviceStep(self string) Step {
	var acts []Action
	if self != mpnBinary {
		acts = append(acts, Run("install", "-m", "0755", self, mpnBinary))
	}
	return Step{"renewal and backup timers", append(acts,
		WriteFile("/etc/systemd/system/mpn-renew.service", renewService, 0o644),
		WriteFile("/etc/systemd/system/mpn-renew.timer", renewTimer, 0o644),
		WriteFile("/etc/systemd/system/mpn-backup.service", backupService, 0o644),
		WriteFile("/etc/systemd/system/mpn-backup.timer", backupTimer, 0o644),
		Run("systemctl", "daemon-reload"),
		Run("systemctl", "enable", "--now", "mpn-renew.timer"),
		Run("systemctl", "enable", "--now", "mpn-backup.timer"),
	)}
}

// The nightly backup runs at low CPU and disk priority so that it does not slow the sites on a small server.
// It keeps the 7 newest backups of every site.
const (
	backupService = "[Unit]\nDescription=Back up all sites\n\n[Service]\nType=oneshot\nNice=10\nIOSchedulingClass=idle\nExecStart=" + mpnBinary + " backup create -all -keep 7 -apply\n"
	backupTimer   = "[Unit]\nDescription=Back up all sites every night\n\n[Timer]\nOnCalendar=*-*-* 03:30:00\nRandomizedDelaySec=30min\nPersistent=true\n\n[Install]\nWantedBy=timers.target\n"
)

func nginxConf(p tune.Profile) string {
	return fmt.Sprintf(`user `+distro.WebUser()+`;
worker_processes %d;
pid /run/nginx.pid;
include /etc/nginx/modules-enabled/*.conf;
events { worker_connections %d; }
http {
    sendfile on;
    tcp_nopush on;
    keepalive_timeout 65;
    server_tokens off;
    ssl_protocols TLSv1.2 TLSv1.3;
    include /etc/nginx/mime.types;
    default_type application/octet-stream;
    access_log off;
    error_log /var/log/nginx/error.log;
    gzip on;
    gzip_vary on;
    gzip_comp_level 5;
    gzip_types text/plain text/css application/json application/javascript text/xml application/xml image/svg+xml;
    map $http_upgrade $connection_upgrade { default upgrade; '' close; }
    include /etc/nginx/conf.d/*.conf;
    include /etc/nginx/sites-enabled/*;
}
`, p.NginxWorkers, p.WorkerConnections)
}

// String renders the plan as a readable dry run.
func (p Plan) String() string {
	var b strings.Builder
	for i, s := range p {
		fmt.Fprintf(&b, "%d. %s\n", i+1, s.Name)
		for _, a := range s.Actions {
			if a.Args != nil {
				cmd := strings.Join(a.Args, " ")
				if i := strings.IndexByte(cmd, '\n'); i >= 0 {
					cmd = cmd[:i] + " ... (multi-line script)"
				}
				fmt.Fprintf(&b, "   run   %s\n", cmd)
			} else {
				fmt.Fprintf(&b, "   write %s (%d bytes, %04o)\n", a.Path, len(a.Content), a.Mode)
			}
		}
	}
	return b.String()
}
