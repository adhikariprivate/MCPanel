// Package site defines the hosted-site model and validates it before anything is rendered or stored.
package site

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

type Kind string

const (
	Static Kind = "static"
	PHP    Kind = "php"
	Proxy  Kind = "proxy"
	Binary Kind = "binary"
	Mise   Kind = "mise"
)

var miseTaskRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

// ValidMiseTask reports whether name is safe as one argv item to mise run.
func ValidMiseTask(name string) bool { return miseTaskRe.MatchString(name) }

// MiseDataDir contains mutable mise metadata and caches for one site.
func MiseDataDir(name string) string { return "/var/lib/mpn/mise/" + name + "/data" }

// MiseInstallsDir separates executable runtimes so their directory can carry the EL executable label.
func MiseInstallsDir(name string) string { return "/var/lib/mpn/mise/" + name + "/installs" }

// PHPVersions are the versions offered. 8.6 is left out until it is confirmed as a stable release.
var PHPVersions = []string{"7.0", "7.1", "7.2", "7.3", "7.4", "8.0", "8.1", "8.2", "8.3", "8.4", "8.5"}

var entryRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._/-]*$`)

type Site struct {
	ID         int64  `json:"-"`
	AccessID   string `json:"-"` // random stable identity; unlike a rowid, it cannot be reused for a recreated site
	Name       string
	Kind       Kind
	User       string   // system account that owns the files and runs the app
	Root       string   // absolute document root
	Hostnames  []string // the first one is the primary name
	PHPVersion string   // php only
	AppPort    int      // app port for Binary and mise sites
	Entry      string   // binary executable relative to the site's home
	MiseTask   string   // mise task selected as the supervised web process; empty means not configured
	Upstream   string   // proxy only, an http or https URL
	TLS        bool     // HTTPS is on; set once a certificate is installed
	// Settings a person may change later (see settings.go).
	PHPIni map[string]string // php only: the settings that differ from the defaults; validated against a fixed list
	MemMB  int               // app memory limit in MB; 0 is the default
	// The domain choices (see domain.go).
	WWW       string // WWWOff, WWWBare or WWWWith
	AllowHTTP bool   // with SSL on, still serve over plain HTTP instead of sending visitors to SSL
	SSLMode   string // SSLNone, SSLLetsEncrypt or SSLCustom; TLS is true exactly when this is not SSLNone
	// CustomVhost is the whole nginx configuration of the site when a person wrote it by hand on the vHost tab. It replaces the
	// one mpn makes, so settings that mpn writes into that one (domain, SSL, port, web root) no longer reach it.
	CustomVhost string
	// Speed turns on compression and long browser caching of static files (see vhost.go).
	Speed bool
	// Webmail serves the web mail under /webmail/ of the site's own names. It needs SSL on the site: a password must not cross the
	// Internet in the clear (see vhost.WebmailLocations).
	Webmail bool
	// App is the application mpn installed in a PHP site, one of Apps, or "" for a site whose code the person brought. It decides
	// which tabs and cards the site has (WordPress has a manager) and is set only once the installation has finished.
	App string
	// AppReady is set once the application finished its own setup in the browser (phpBB: an administrator exists in its database).
	// Until then the vhost of phpBB serves its installer in /install/; from then on it does not, and the folder is removed.
	AppReady bool
}

// Apps are the applications mpn can download and set up in a PHP site.
var Apps = []string{"wordpress", "laravel", "codeigniter", "cakephp", "yii", "phpbb"}

// ValidApp reports whether id names an application of Apps.
func ValidApp(id string) bool { return slices.Contains(Apps, id) }

// reservedUsers are accounts that exist on a stock Debian or Ubuntu server or that the software installed by mpn runs
// as. A site's account must be created for it, never one of these. Any other existing account is refused when the site
// is applied, because it does not carry the mark mpn puts on the accounts it creates.
var reservedUsers = map[string]bool{
	"root": true, "daemon": true, "bin": true, "sys": true, "sync": true, "games": true, "man": true, "lp": true,
	"mail": true, "news": true, "uucp": true, "proxy": true, "www-data": true, "backup": true, "list": true,
	"irc": true, "nobody": true, "nginx": true, "mysql": true, "mariadb": true, "redis": true, "memcache": true,
	"postfix": true, "sshd": true, "ubuntu": true, "debian": true, "admin": true, "administrator": true,
}

var (
	nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)
	userRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	pathRe = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)
	hostRe = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
)

// IsReservedUser reports whether u is a system account name that a site may not use.
func IsReservedUser(u string) bool { return reservedUsers[u] || strings.HasPrefix(u, "systemd-") }

// ReservedUsers lists the reserved names in order, for forms that want to avoid them as the user types.
func ReservedUsers() []string { return slices.Sorted(maps.Keys(reservedUsers)) }

// ValidName reports whether n is an acceptable site name.
func ValidName(n string) bool { return nameRe.MatchString(n) }

// ValidHostname reports whether h is a lower-case ASCII host name that is safe to place in configuration.
func ValidHostname(h string) bool { return hostRe.MatchString(h) }

// SafePath reports whether p is an absolute path made only of characters that are harmless in nginx config.
func SafePath(p string) bool { return pathRe.MatchString(p) && !strings.Contains(p, "..") }

// Validate returns every problem found, joined, or nil when the site is safe to render.
func (s Site) Validate() error {
	var errs []error
	add := func(f string, a ...any) { errs = append(errs, fmt.Errorf(f, a...)) }
	if !nameRe.MatchString(s.Name) {
		add("name %q: use a-z, 0-9 and '-', at most 31 characters", s.Name)
	}
	if !userRe.MatchString(s.User) {
		add("user %q is not a valid account name", s.User)
	} else if IsReservedUser(s.User) {
		add("user %q is a system account; every site gets its own new account, so pick another name", s.User)
	}
	if !SafePath(s.Root) {
		add("root %q must be an absolute path without '..' or special characters", s.Root)
	}
	if len(s.Hostnames) == 0 {
		add("at least one hostname is required")
	}
	for _, h := range s.Hostnames {
		if !hostRe.MatchString(h) {
			add("hostname %q is not a valid lower-case ASCII name", h)
		}
	}
	for _, p := range s.validDomainFields() {
		add("%s", p)
	}
	switch s.Kind {
	case Static:
	case PHP:
		if !slices.Contains(PHPVersions, s.PHPVersion) {
			add("php version %q is not offered", s.PHPVersion)
		}
		if err := ValidatePHPIni(s.PHPIni); err != nil {
			add("php settings: %v", err)
		}
		if s.App != "" && !ValidApp(s.App) {
			add("application %q is not one mpn installs", s.App)
		}
	case Binary:
		if !ValidAppPort(s.AppPort) {
			add("binary app port %d must be between 1024 and 65535 and not one that the server itself uses", s.AppPort)
		}
		if !ValidBinaryEntry(s.Entry) {
			add("binary entry %q must be a relative executable path under the site's home", s.Entry)
		}
		if !ValidMemMB(s.MemMB) {
			add("memory %d MB must be 0 (the default) or between 64 and 8192", s.MemMB)
		}
	case Mise:
		if s.Root != HtdocsOf(s.User) {
			add("mise sites use the fixed web root %q so mise.toml stays editable in the site file tools", HtdocsOf(s.User))
		}
		if !ValidAppPort(s.AppPort) {
			add("mise app port %d must be between 1024 and 65535 and not one that the server itself uses", s.AppPort)
		}
		if s.MiseTask != "" && !ValidMiseTask(s.MiseTask) {
			add("mise task %q has an invalid name", s.MiseTask)
		}
		if !ValidMemMB(s.MemMB) {
			add("memory %d MB must be 0 (the default) or between 64 and 8192", s.MemMB)
		}
	case Proxy:
		if u, err := url.Parse(s.Upstream); err != nil || (u.Scheme != "http" && u.Scheme != "https") ||
			u.Host == "" || strings.ContainsAny(s.Upstream, " \t\r\n;{}\"'\\$") {
			add("upstream %q must be a plain http or https URL", s.Upstream)
		}
	default:
		add("unknown kind %q", s.Kind)
	}
	return errors.Join(errs...)
}

// ProxyTarget returns the URL nginx forwards to, or "" for kinds that are not proxied.
func (s Site) ProxyTarget() string {
	switch s.Kind {
	case Binary, Mise:
		return fmt.Sprintf("http://127.0.0.1:%d", s.AppPort)
	case Proxy:
		return s.Upstream
	}
	return ""
}

// reservedPorts are ports the server's own services use. An app must not be given one: it would either fail to start or,
// worse, take the place of the service.
var reservedPorts = map[int]bool{
	22: true, 25: true, 80: true, 443: true, 465: true, 587: true, 993: true, 995: true, 3306: true, 33060: true, 5432: true,
	6379: true, 8780: true, 2610: true, 2611: true, 9000: true, 11211: true,
	// The web mail: its public port (the panel's plus two) and the port its program listens on behind nginx.
	2612: true, 8781: true,
}

// ValidAppPort reports whether an app may listen on p.
func ValidAppPort(p int) bool { return p >= 1024 && p <= 65535 && !reservedPorts[p] }

func ValidPHPVersion(v string) bool { return slices.Contains(PHPVersions, v) }

// ValidBinaryEntry permits an executable path under the site's home, outside the public document root.
func ValidBinaryEntry(e string) bool { return entryRe.MatchString(e) && !strings.Contains(e, "..") }

// ValidUpstream reports whether u is a plain http or https address that is safe to place in the web server's settings.
func ValidUpstream(u string) bool {
	p, err := url.Parse(u)
	return err == nil && (p.Scheme == "http" || p.Scheme == "https") && p.Host != "" && !strings.ContainsAny(u, " \t\r\n;{}\"'\\$")
}

// ValidMemMB reports whether m is 0 (the default) or an application memory limit.
func ValidMemMB(m int) bool { return m == 0 || (m >= 64 && m <= 8192) }
