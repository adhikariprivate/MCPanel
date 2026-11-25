// Package web is the panel's HTTP side. It runs without privileges and never touches the machine itself: every change
// is a ctl.Request sent to the root helper.
package web

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/pkg/sftp"
	"time"

	"mpn/internal/ctl"
	"mpn/internal/monitor"
	"mpn/internal/webauth"
)

type Server struct {
	Account  func() (webauth.Account, error) // read on every login, so a new password applies at once
	Sessions *webauth.Sessions
	Throttle *webauth.Throttle
	Hosts    []string // names the panel answers to, besides localhost; any other Host header is refused
	// HostsFile lists more such names, one per line. It is read while the panel runs, so a name added by the SSL page is
	// accepted at once.
	HostsFile  string
	Secure     bool // the panel is reached over HTTPS: sets the Secure cookie flag and HSTS
	TrustProxy bool // take the client address from X-Real-IP, which the local nginx sets
	// Call sends a request to the root helper. The web side has no other way to change anything.
	Call func(ctl.Request) (ctl.Response, error)
	// UserExists says whether the machine already has an account with that name; the system's user database when nil.
	UserExists func(string) bool
	// LoginCheck says whether the panel can log in as that account with its own key; nil means it is not checked.
	LoginCheck func(user string) error
	// PasswdFile is where the shell of an account is read from; /etc/passwd when empty.
	PasswdFile string
	// InstalledPHP tells which versions are installed, so the form offers only those. When nil, the form uses known PHP versions.
	InstalledPHP func() []string
	// Mon takes the measurements the monitoring page draws; nil leaves the page empty. UnitState asks systemd about a service;
	// the real systemd when nil.
	Mon       *monitor.Collector
	UnitState monitor.UnitState
	// Dialer logs in as a website's account for the terminal; nil switches the terminal off.
	Dialer            Dialer
	TermIdle, TermMax time.Duration // a terminal closes after this long without typing, and after this long in all
	MaxTerminals      int           // open terminals at once; 8 when 0
	TermLog           func(event, site string, err error)
	terms             atomic.Int32
	// PMAPort is the port of the database browser (phpMyAdmin) on this machine, 0 when it is not installed. PMAKey is the
	// key that its sign-on page proves itself with; the browser is off when the key is shorter than 32 bytes.
	PMAPort int
	PMAKey  []byte
	tickets pmaTickets
	// MaxUpload is the largest file the file manager takes in one upload; 1 GiB when 0.
	MaxUpload int64
	// FMRoot finds the home directory the file manager works in; the SFTP session's start directory when nil.
	FMRoot func(*sftp.Client) (string, error)

	// SaveAccount writes the administrator's account (a changed password, the secret of the two-step sign-in). The account file
	// belongs to the panel's own account, so no help from the root helper is needed; nil switches those settings off.
	SaveAccount func(webauth.Account) error
	// CreateAccount writes the first account, and fails with fs.ErrExist when an account file is there already. SetupKeyFile holds the
	// key of the setup link (see setuppage.go); without it the setup page stays closed.
	CreateAccount func(webauth.Account) error
	SetupKeyFile  string
	// accountMu makes reading the account, checking a code against it and saving the used step one step, so a code works once.
	accountMu  sync.Mutex
	accessMu   sync.Mutex
	AccessFile string // panel-owned delegated users and per-site grants; empty disables delegated access
	twofa      twoFactor
	// PanelDomainFile is the nginx file of the panel's name on port 443. When it exists and a request came to that plain address, the
	// database browser is opened under /pma/ of the name and not on its own port.
	PanelDomainFile string
	// NotifySocket is the socket of the mpn-notify service, which takes the reports of other machines when this panel is the hub of the
	// Telegram alerts; "" switches /_notify/ off.
	NotifySocket string
	cf           cfCache // what the name servers of mail domains said, for ten minutes

	flashMu   sync.Mutex
	flash     map[string]*result
	pairCodes map[string][2]string // a pairing code of the alerts, shown once, by session
}

const maxBody = 64 << 10

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /language/{language}", s.changeLanguage)
	s.setupPages(mux)
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		if s.firstRun() {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		s.render(w, 200, view{Page: "login"})
	})
	mux.HandleFunc("POST /login", s.login)
	s.accessPages(mux)
	s.settingsPages(mux)
	mux.HandleFunc("POST /logout", s.auth(s.logout))
	mux.HandleFunc("GET /{$}", s.auth(s.sitesPage))
	s.sitePages(mux)
	s.dbPages(mux)
	s.firewallPages(mux)
	s.securityPages(mux)
	s.mailPages(mux)
	s.mailCFPages(mux)
	s.monitorPages(mux)
	s.auditPages(mux)
	s.notifyHubRoutes(mux)
	s.notifyPages(mux)
	s.servicesPages(mux)
	s.pmaRoutes(mux)
	s.backupPages(mux)
	s.sitePagesOfOne(mux)
	s.terminalPages(mux)
	s.fileRoutes(mux)
	s.assetRoutes(mux)
	mux.HandleFunc("GET /result", s.auth(s.resultPage))
	mux.HandleFunc("GET /", s.notFound) // every address no other route owns
	return s.guard(mux)
}

// guard runs before every handler: it refuses a Host header the panel does not own (which stops DNS rebinding), refuses
// a form that did not come from the panel's own pages, limits the body, and sets the security headers.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hostOK(r.Host) {
			http.Error(w, "unknown host", http.StatusMisdirectedRequest)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", basePolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Cache-Control", "no-store")
		h.Set("Content-Language", requestLanguage(r))
		h.Add("Vary", "Cookie")
		h.Add("Vary", "Accept-Language")
		if s.Secure {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		// Another machine sends its report here; it carries its own key and no cookie, so a forged form could do nothing with it.
		machine := strings.HasPrefix(r.URL.Path, "/_notify/")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !machine && !sameOrigin(r) {
				http.Error(w, "cross-site request refused", http.StatusForbidden)
				return
			}
			if limit := bodyLimit(r.URL.Path); limit > 0 {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) hostOK(hostport string) bool {
	host := strings.ToLower(hostport)
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = strings.ToLower(h)
	}
	if host == "localhost" {
		return true
	}
	// An IP address is always accepted: DNS rebinding works through a name an attacker controls, and such a name
	// arrives as that name in the Host header, never as an address.
	if net.ParseIP(strings.Trim(host, "[]")) != nil {
		return true
	}
	for _, x := range s.Hosts {
		if host == strings.ToLower(x) {
			return true
		}
	}
	for _, x := range s.fileHosts() {
		if host == x {
			return true
		}
	}
	return false
}

// sameOrigin accepts a request whose Origin is this very host, or that the browser itself marks as same-origin.
func sameOrigin(r *http.Request) bool {
	// Browsers send "Origin: null" for a form post from the page's own origin when the referrer policy hides the
	// referrer. Such a request is accepted only when the browser itself marks it same-origin, which a page on another
	// site cannot forge.
	if o := r.Header.Get("Origin"); o != "" && o != "null" {
		u, err := url.Parse(o)
		return err == nil && u.Host != "" && strings.EqualFold(u.Host, r.Host)
	}
	return r.Header.Get("Sec-Fetch-Site") == "same-origin"
}

func (s *Server) cookieName() string {
	if s.Secure {
		return "__Host-mpn"
	}
	return "mpn"
}

// clientAddr is the address of the visitor: what nginx says in X-Real-IP when the panel trusts it (it listens on a socket only nginx
// can open), else the address of the connection.
func (s *Server) clientAddr(r *http.Request) string {
	if s.TrustProxy {
		if ip := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); ip != nil {
			return ip.String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// clientKey is what the limit on sign-in attempts counts by: the address, and for IPv6 its /64, because one machine is usually given a
// whole /64 and can take a new address in it for every try. The log line fail2ban reads keeps the full address (clientAddr).
func (s *Server) clientKey(r *http.Request) string {
	a := s.clientAddr(r)
	ip := net.ParseIP(a)
	if ip == nil || ip.To4() != nil {
		return a
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// auth wraps a handler that needs a logged-in user. A form post must also carry the session's CSRF token.
func (s *Server) auth(h func(http.ResponseWriter, *http.Request, webauth.Session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(s.cookieName())
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		ss, ok := s.Sessions.Get(c.Value)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if r.Method == http.MethodPost && !s.Sessions.CheckCSRF(c.Value, r.PostFormValue("csrf")) {
			http.Error(w, "invalid form token; go back, reload the page and try again", http.StatusForbidden)
			return
		}
		if !s.authorizeRequest(w, r, ss) {
			return
		}
		h(w, r, ss)
	}
}

func (s *Server) isOwner(user string) bool {
	a, err := s.Account()
	return err == nil && a.User != "" && user == a.User
}

func (s *Server) delegatedAccess() (webauth.DelegatedAccess, error) {
	if s.AccessFile == "" {
		return webauth.NewDelegatedAccess(), nil
	}
	s.accessMu.Lock()
	defer s.accessMu.Unlock()
	return webauth.LoadDelegatedAccess(s.AccessFile)
}

// authorizeRequest is the server-side permission boundary for every authenticated route. Unknown and instance-wide
// routes are owner-only. Site routes require an explicit capability mapping, an assignment, and an explicit allow.
func (s *Server) authorizeRequest(w http.ResponseWriter, r *http.Request, ss webauth.Session) bool {
	if s.isOwner(ss.User) {
		return true
	}
	capability, site, ownerOnly := requestCapability(r)
	if capability == "session.logout" || capability == "session.result" {
		return true
	}
	if capability == "account.settings" || capability == "account.password" {
		access, err := s.delegatedAccess()
		if err == nil {
			if user, ok := access.Users[ss.User]; ok && user.Enabled {
				return true
			}
		}
		return s.denyAccess(w, r, ss, capability, site)
	}
	access, err := s.delegatedAccess()
	if err != nil {
		http.Error(w, "access control unavailable", http.StatusServiceUnavailable)
		return false
	}
	if capability == "sites.list" {
		rows, err := s.loadSiteRows()
		if err != nil {
			http.Error(w, "access control unavailable", http.StatusServiceUnavailable)
			return false
		}
		for _, row := range rows {
			if access.Allows(ss.User, row.AccessID, "site.view") {
				return true
			}
		}
		return s.denyAccess(w, r, ss, capability, site)
	}
	if ownerOnly || capability == "" || (site == "" && capability != "activity.view") {
		return s.denyAccess(w, r, ss, capability, site)
	}
	rows, err := s.loadSiteRows()
	if err != nil {
		http.Error(w, "access control unavailable", http.StatusServiceUnavailable)
		return false
	}
	if site == "" && capability == "activity.view" {
		for _, row := range rows {
			if row.AccessID != "" && access.Allows(ss.User, row.AccessID, "site.view") {
				return true
			}
		}
		return s.denyAccess(w, r, ss, capability, site)
	}
	for _, row := range rows {
		if row.Name == site && row.AccessID != "" && (access.Allows(ss.User, row.AccessID, capability) || (capability == "site.domain" && (access.Allows(ss.User, row.AccessID, "site.settings") || access.Allows(ss.User, row.AccessID, "ssl.view"))) || (capability == "site.advanced" && (access.Allows(ss.User, row.AccessID, "site.settings") || access.Allows(ss.User, row.AccessID, "vhost.view") || access.Allows(ss.User, row.AccessID, "vhost.manage"))) || (capability == "background.view" && access.Allows(ss.User, row.AccessID, "background.manage"))) {
			return true
		}
	}
	return s.denyAccess(w, r, ss, capability, site)
}

func (s *Server) denyAccess(w http.ResponseWriter, r *http.Request, ss webauth.Session, capability, site string) bool {
	path := strings.Trim(r.URL.Path, "/")
	sensitive := capability == "terminal.open" || capability == "ssh.manage" || capability == "files.delete" || capability == "backup.restore" ||
		path == "sites/remove" || path == "databases/remove" || path == "databases/passwd" || path == "backups/restore"
	if r.Method != http.MethodGet || sensitive {
		s.note(r, "access.denied", site, fmt.Sprintf("by=%s capability=%s path=%s", ss.User, capability, path), true)
	}
	http.Error(w, "not authorized", http.StatusForbidden)
	return false
}

func requestCapability(r *http.Request) (capability, site string, ownerOnly bool) {
	p := strings.Trim(r.URL.Path, "/")
	if p == "settings" && r.Method == http.MethodGet {
		return "account.settings", "", false
	}
	if p == "settings/password" && r.Method == http.MethodPost {
		return "account.password", "", false
	}
	if strings.HasPrefix(p, "settings/") || p == "update" || strings.HasPrefix(p, "update/") {
		return "", "", true
	}
	if p == "logout" {
		return "session.logout", "", false
	}
	if p == "result" {
		return "session.result", "", false
	}
	if p == "" {
		return "sites.list", "", false
	}
	if p == "activity" || p == "activity.csv" {
		return "activity.view", "", false
	}
	if p == "sites/new" || p == "sites/remove" {
		return "", "", true
	}
	parts := strings.Split(p, "/")
	if len(parts) < 2 || parts[0] != "sites" || parts[1] == "" {
		return "", "", true
	}
	site = r.PathValue("name")
	if site == "" {
		site = parts[1]
	}
	if len(parts) == 2 {
		if r.Method == http.MethodGet {
			return "site.view", site, false
		}
		return "", "", true
	}
	suffix := strings.Join(parts[2:], "/")
	if r.Method == http.MethodGet {
		switch suffix {
		case "files", "files/download", "files/rename", "files/edit":
			return "files.read", site, false
		case "terminal", "terminal/ws":
			return "terminal.open", site, false
		case "ssh", "ssh/reset", "ssh/disable":
			return "site.view", site, false
		case "ssl":
			return "ssl.view", site, false
		case "domain":
			return "site.domain", site, false
		case "sftp":
			return "site.view", site, false
		case "advanced":
			return "site.advanced", site, false
		case "cron":
			return "cron.view", site, false
		case "background":
			return "background.view", site, false
		case "logs":
			return "logs.view", site, false
		case "databases":
			return "database.view", site, false
		case "backups":
			return "backup.view", site, false
		case "email", "email/cf":
			return "email.view", site, false
		case "wordpress", "wp/status":
			return "wordpress.view", site, false
		case "cloud/backups":
			return "cloud.view", site, false
		case "ssh/enable":
			return "", "", true
		}
		return "", "", true
	}
	if r.Method != http.MethodPost {
		return "", "", true
	}
	switch suffix {
	case "files/upload", "files/mkdir", "files/newfile", "files/rename", "files/zip", "files/unzip", "files/paste", "files/save":
		return "files.write", site, false
	case "files/delete", "files/confirm-delete":
		return "files.delete", site, false
	case "files/chmod":
		return "files.permissions", site, false
	case "ssh/enable", "ssh/reset", "ssh/disable", "ssh/panelkey":
		return "ssh.manage", site, false
	case "ssl/letsencrypt", "ssl/custom", "ssl/csr", "ssl/off", "ssl/redirect":
		return "ssl.manage", site, false
	case "cron":
		return "cron.manage", site, false
	case "background":
		return "background.manage", site, false
	case "domain", "settings", "speed", "webmail":
		return "site.settings", site, false
	case "mise-task":
		return "site.settings", site, false
	case "vhost", "vhost/reset":
		return "vhost.manage", site, false
	case "email/domain/add", "email/domain/delete", "email/box/add", "email/box/passwd", "email/box/quota", "email/box/delete", "email/alias/set", "email/alias/delete", "email/cf":
		return "email.manage", site, false
	case "wp/run":
		return "wordpress.manage", site, false
	case "app/install":
		return "app.manage", site, false
	case "cloud/connect", "cloud/configure", "cloud/finish", "cloud/enable", "cloud/sync", "cloud/disconnect", "cloud/fetch":
		return "cloud.manage", site, false
	}
	if len(parts) == 5 && parts[2] == "cron" && (parts[4] == "toggle" || parts[4] == "delete") {
		if id, err := strconv.ParseInt(parts[3], 10, 64); err == nil && id > 0 {
			return "cron.manage", site, false
		}
	}
	if len(parts) == 5 && parts[2] == "background" && (parts[4] == "start" || parts[4] == "stop" || parts[4] == "restart" || parts[4] == "delete") {
		if id, err := strconv.ParseInt(parts[3], 10, 64); err == nil && id > 0 {
			return "background.manage", site, false
		}
	}
	return "", "", true
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	T := translatorForWriter(w)
	key := s.clientKey(r)
	locked := func(d time.Duration) {
		w.Header().Set("Retry-After", strconv.Itoa(int(d.Seconds())+1))
		s.render(w, http.StatusTooManyRequests, view{Page: "login", Error: T("login.locked", int(d.Seconds())+1)})
	}
	release, d := s.Throttle.Admit(key)
	if d > 0 {
		locked(d)
		return
	}
	defer release()
	user, pw := r.PostFormValue("user"), r.PostFormValue("password")
	acct, err := s.Account()
	good := false
	if err == nil && len(user) <= 64 && len(pw) <= 256 {
		var busy error
		if user == acct.User {
			good, busy = acct.CheckLimited(user, pw)
		} else if access, accessErr := s.delegatedAccess(); accessErr == nil {
			good, busy = access.CheckLimited(user, pw)
		}
		if busy != nil {
			locked(2 * time.Second)
			return
		}
	}
	if !good {
		s.Throttle.Fail(key)
		s.note(r, "login.fail", "", "user="+auditUser(user, acct.User), true)
		log.Printf("login failed from %s", s.clientAddr(r)) // read by the mpn-panel jail of fail2ban
		s.render(w, http.StatusUnauthorized, view{Page: "login", Error: T("login.wrong")})
		return
	}
	if acct.User == user && acct.TOTP != "" { // the password is right; the code of the app is still to come
		s.render(w, http.StatusOK, view{Page: "login", Need2FA: true, Pending: s.twofa.newLogin(acct.User)})
		return
	}
	s.note(r, "login.ok", "", "user="+auditUser(user, user), false)
	s.Throttle.Reset(key)
	s.startSession(w, r, user)
}

// notFound answers an address the panel does not have with a page of its own. It is the same page for a signed-in person and for a
// stranger, and says nothing about what exists.
func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusNotFound, view{Page: "notfound"})
}

// startSession signs the browser in: a new session, and its cookie.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, user string) {
	if c, err := r.Cookie(s.cookieName()); err == nil {
		s.Sessions.Delete(c.Value) // a login never reuses an older session
	}
	tok, _ := s.Sessions.Create(user)
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Value: tok, Path: "/", HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request, ss webauth.Session) {
	s.note(r, "logout", "", "user="+ss.User, false)
	if c, err := r.Cookie(s.cookieName()); err == nil {
		s.Sessions.Delete(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

type view struct {
	Page, User, CSRF, Error, Title, Nav, Kind   string
	Owner                                       bool
	ShowActivity                                bool
	SetupLocked                                 bool // the setup page was opened without the key of the setup link
	Sites                                       []siteRow
	Kinds                                       []string
	Reserved                                    string
	Password                                    string
	Nonce                                       string
	Dir, FileName, EditPath, EditText, FileKind string
	DBBase                                      string    // the start every database name of the site has
	DBEngines                                   []string  // engines installed on this machine
	PMA                                         bool      // the database browser is installed: show its button
	Over                                        *overview // the overview tab of a site: its state and its settings
	PMALink, PMAName                            string    // where the interstitial page leads, and the database it opens
	EditMtime                                   int64
	CRLF, Cut                                   bool
	Notice                                      *result
	Files                                       []fileRow
	Crumbs                                      []crumb
	Parent                                      string
	Site                                        siteRow
	SiteOptions                                 []siteRow
	Tab                                         string
	TabAccess                                   map[string]bool
	SSHEnabled                                  bool
	KeyStatus                                   string
	SSHHost                                     string
	ServerIP                                    string // shown in the header of a site
	EmailEnabled                                bool
	Dom                                         *domainView
	SSL                                         *sslView
	VH                                          *vhostView
	Cron                                        *cronView
	Background                                  *backgroundView
	Log                                         *logView
	Svc                                         *svcView
	CF                                          *cfView
	SM                                          *siteMailView
	Settings                                    *settingsView
	Need2FA                                     bool   // the sign-in page asks for the code of the app
	Pending                                     string // the sign-in that waits for it
	FW                                          *fwView
	Audit                                       *auditView
	Notify                                      *notifyView
	Sec                                         *secView
	Mail                                        *mailView
	Cloud                                       *cloudView
	CloudList                                   *cloudListView
	WP                                          *wpView
	Apps                                        []appOption
	Locales                                     []string
	Mon                                         *monView
	DBs                                         []dbRow
	Backups                                     []backupRow
	SiteNames                                   []string
	PHPVersions                                 []string
	Result                                      *result
	Confirm                                     *confirm
	AccessUsers                                 []accessUserRow
	AccessSites                                 []accessSiteView
	AccessSite                                  string
	AccessUsername                              string
	AccessEnabled                               bool
	AccessEditing                               bool
	AccessPeople                                []accessPersonView
}

func (s *Server) render(w http.ResponseWriter, status int, v view) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	s.execute(w, v)
}

// bodyLimit is how much of a request body the panel reads. Forms are small. An upload is read as a stream by its own
// handler, which stops at its own limit, and the text of the editor may be as large as the largest file it opens.
func bodyLimit(p string) int64 {
	switch {
	case strings.HasPrefix(p, "/sites/") && strings.HasSuffix(p, "/files/upload"):
		return 0
	case strings.HasPrefix(p, "/sites/") && strings.HasSuffix(p, "/files/save"):
		return 6 << 20
	case strings.HasPrefix(p, "/sites/") && strings.HasSuffix(p, "/vhost"):
		return 256 << 10 // up to 48 KB of configuration; the encoding of a form can make every character three bytes long
	case strings.HasPrefix(p, "/sites/") && strings.HasSuffix(p, "/ssl/custom"):
		return 128 << 10 // three boxes of up to 16 KB, which the encoding of the form makes larger
	case strings.HasPrefix(p, "/_notify/"):
		return notifyRelayLimit
	}
	return maxBody
}
