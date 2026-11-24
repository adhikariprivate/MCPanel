package web

import (
	"net/http"

	"mpn/internal/access"
	"mpn/internal/apps"
	"mpn/internal/ctl"
	"mpn/internal/dbadmin"
	"mpn/internal/services"
	"mpn/internal/site"
	"mpn/internal/webauth"
)

// siteTabLink is one entry of the menu of a site: the key names its icon and its label (tab.<key>), the path follows /sites/<name>.
type siteTabLink struct{ Key, Path string }

// siteMenu is the menu of a site, most used first.
func siteMenu(r siteRow, permissions map[string]bool, emailEnabled bool) []siteTabLink {
	if permissions == nil {
		permissions = map[string]bool{}
	}
	menu := []siteTabLink{{"overview", ""}, {"files", "/files"}, {"databases", "/databases"}, {"email", "/email"}, {"domain", "/domain"}, {"sftp", "/sftp"}, {"backups", "/backups"}, {"cron", "/cron"}, {"logs", "/logs"}, {"terminal", "/terminal"}, {"advanced", "/advanced"}}
	permissions["email"] = permissions["email"] && emailEnabled
	permissions["domain"] = permissions["site.settings"] || permissions["domain"]
	permissions["advanced"] = permissions["site.settings"] || permissions["vhost.view"] || permissions["vhost.manage"]
	filtered := menu[:0]
	for _, item := range menu {
		if permissions[item.Key] {
			filtered = append(filtered, item)
		}
	}
	menu = filtered
	if r.App == "wordpress" { // the manager comes right after the overview: it is what a WordPress site is mostly used through
		if permissions["wordpress"] {
			for i, item := range menu {
				if item.Key == "overview" {
					menu = append(menu[:i+1], append([]siteTabLink{{"wordpress", "/wordpress"}}, menu[i+1:]...)...)
					break
				}
			}
		}
	}
	return menu
}

func (s *Server) siteTabAccess(user string, row siteRow) map[string]bool {
	capabilities := map[string]string{
		"overview": "site.view", "files": "files.read", "databases": "database.view", "email": "email.view",
		"domain": "ssl.view", "ssl": "ssl.view", "backups": "backup.view", "cron": "cron.view", "logs": "logs.view", "sftp": "site.view", "advanced": "site.settings",
		"terminal": "terminal.open", "vhost.view": "vhost.view", "wordpress": "wordpress.view",
		"site.settings": "site.settings", "ssh.manage": "ssh.manage", "app.manage": "app.manage",
		"email.manage": "email.manage", "ssl.manage": "ssl.manage", "cron.manage": "cron.manage", "vhost.manage": "vhost.manage",
		"files.write": "files.write", "files.delete": "files.delete", "files.permissions": "files.permissions", "cloud.manage": "cloud.manage",
		"wordpress.manage": "wordpress.manage",
	}
	out := make(map[string]bool, len(capabilities))
	if s.isOwner(user) {
		for key := range capabilities {
			out[key] = true
		}
		return out
	}
	access, err := s.delegatedAccess()
	if err != nil {
		return out
	}
	for key, capability := range capabilities {
		out[key] = access.Allows(user, row.AccessID, capability)
	}
	out["advanced"] = out["site.settings"] || out["vhost.view"] || out["vhost.manage"]
	return out
}

func (s *Server) sitePagesOfOne(mux *http.ServeMux) {
	mux.HandleFunc("GET /sites/{name}/access", s.auth(s.siteAccessPage))
	for _, tab := range []string{"", "/domain", "/sftp", "/advanced", "/databases", "/backups", "/cron", "/logs", "/email", "/wordpress"} {
		tab := tab
		mux.HandleFunc("GET /sites/{name}"+tab, s.auth(func(w http.ResponseWriter, r *http.Request, ss webauth.Session) {
			s.siteTab(w, r, ss, map[string]string{"": "overview", "/domain": "domain", "/sftp": "sftp", "/advanced": "advanced", "/databases": "databases", "/backups": "backups", "/cron": "cron", "/logs": "logs", "/email": "email", "/wordpress": "wordpress"}[tab])
		}))
	}
	mux.HandleFunc("POST /sites/{name}/cron", s.auth(s.addCron))
	mux.HandleFunc("POST /sites/{name}/cron/{id}/toggle", s.auth(s.toggleCron))
	mux.HandleFunc("POST /sites/{name}/cron/{id}/delete", s.auth(s.deleteCron))
	mux.HandleFunc("POST /sites/{name}/vhost", s.auth(s.saveVhost))
	mux.HandleFunc("POST /sites/{name}/vhost/reset", s.auth(s.resetVhost))
	mux.HandleFunc("POST /sites/{name}/domain", s.auth(s.saveDomain))
	mux.HandleFunc("POST /sites/{name}/ssl/letsencrypt", s.auth(s.sslLetsEncrypt))
	mux.HandleFunc("POST /sites/{name}/ssl/custom", s.auth(s.sslCustom))
	mux.HandleFunc("POST /sites/{name}/ssl/csr", s.auth(s.sslCSR))
	mux.HandleFunc("POST /sites/{name}/ssl/off", s.auth(s.sslOff))
	mux.HandleFunc("POST /sites/{name}/ssl/redirect", s.auth(s.sslRedirect))
	mux.HandleFunc("POST /sites/{name}/settings", s.auth(s.saveSettings))
	mux.HandleFunc("POST /sites/{name}/mise-task", s.auth(s.saveMiseTask))
	mux.HandleFunc("POST /sites/{name}/speed", s.auth(s.saveSpeed))
	mux.HandleFunc("POST /sites/{name}/webmail", s.auth(s.saveWebmail))
	// The mail of a website, as in cPanel: the same handlers as the Email page, but only for the website's own names, and back to its tab.
	for path, h := range map[string]func(http.ResponseWriter, *http.Request, webauth.Session){
		"POST /sites/{name}/email/domain/add":    s.mailDomainAdd,
		"POST /sites/{name}/email/domain/delete": s.mailDomainDel,
		"POST /sites/{name}/email/box/add":       s.mailBoxAdd,
		"POST /sites/{name}/email/box/passwd":    s.mailBoxPasswd,
		"POST /sites/{name}/email/box/quota":     s.mailBoxQuota,
		"POST /sites/{name}/email/box/delete":    s.mailBoxDel,
		"POST /sites/{name}/email/alias/set":     s.mailAliasSet,
		"POST /sites/{name}/email/alias/delete":  s.mailAliasDel,
		"GET /sites/{name}/email/cf":             s.cfPage,
		"POST /sites/{name}/email/cf":            s.cfAdd,
	} {
		mux.HandleFunc(path, s.auth(s.siteMailPost(h)))
	}
	s.cloudRoutes(mux)
	s.appRoutes(mux)
	s.wpRoutes(mux)
	mux.HandleFunc("POST /sites/{name}/ssh/enable", s.auth(s.sshEnable))
	mux.HandleFunc("GET /sites/{name}/ssh/reset", s.auth(s.sshConfirm("reset")))
	mux.HandleFunc("POST /sites/{name}/ssh/reset", s.auth(s.sshAct("reset")))
	mux.HandleFunc("GET /sites/{name}/ssh/disable", s.auth(s.sshConfirm("disable")))
	mux.HandleFunc("POST /sites/{name}/ssh/disable", s.auth(s.sshAct("disable")))
	mux.HandleFunc("POST /sites/{name}/ssh/panelkey", s.auth(s.sshPanelKey))
}
func (s *Server) findSite(name string) (siteRow, int, string) {
	return s.findSiteText(T, name)
}

// findSite looks the site up in the helper's list. The name in the address is checked first, so only a valid name is
// ever sent on.
func (s *Server) findSiteText(T translator, name string) (siteRow, int, string) {
	if !site.ValidName(name) {
		return siteRow{}, http.StatusNotFound, T("err.nosite")
	}
	var rows []siteRow
	if e := s.listText(T, ctl.Request{Op: "site.list"}, &rows); e != "" {
		return siteRow{}, http.StatusBadGateway, e
	}
	for _, row := range rows {
		if row.Name == name {
			return row, 0, ""
		}
	}
	return siteRow{}, http.StatusNotFound, T("err.nosite")
}

func (s *Server) passwdFile() string {
	if s.PasswdFile != "" {
		return s.PasswdFile
	}
	return "/etc/passwd"
}

func (s *Server) siteTab(w http.ResponseWriter, r *http.Request, ss webauth.Session, tab string) {
	T := translatorForWriter(w)
	row, status, msg := s.findSiteText(T, r.PathValue("name"))
	if status != 0 {
		http.Error(w, msg, status)
		return
	}
	v := view{Page: "site", Site: row, Tab: tab, ServerIP: serverIP(r.Host), TabAccess: s.siteTabAccess(ss.User, row)}
	mailState := &mailView{}
	v.EmailEnabled = s.listText(T, ctl.Request{Op: "mail.status"}, mailState) == "" && mailState.On
	switch tab {
	case "overview":
		var e string
		if v.Over, e = s.loadOverviewText(T, row); e != "" {
			v.Error = e
		}
		v.Apps, v.Locales = appOptions(), apps.Locales
	case "domain":
		v.Dom = newDomainView(row)
		v.SSL = s.loadSSLText(T, row, v.ServerIP)
	case "sftp":
		s.loadSSH(&v, r, row)
	case "advanced":
		var e string
		if v.Over, e = s.loadOverviewText(T, row); e != "" {
			v.Error = e
		}
		v.VH = s.loadVhostText(T, row)
	case "cron":
		v.Cron = s.loadCronText(T, row)
	case "logs":
		v.Log = s.loadLogs(r, row)
	case "email":
		v.SM = s.loadSiteMailText(T, row, r.Host)
	case "databases":
		v.DBBase = dbadmin.BaseName(row.Name)
		v.PMA = s.pmaOn()
		v.Error = s.listText(T, ctl.Request{Op: "db.list", Site: row.Name}, &v.DBs)
		for i := range v.DBs {
			if v.DBs[i].Engine == "" {
				v.DBs[i].Engine = "mysql"
			}
		}
		var installed []services.Item
		if s.listText(T, ctl.Request{Op: "services.status"}, &installed) == "" {
			for _, item := range installed {
				if item.Installed && item.Key == "database" {
					v.DBEngines = append(v.DBEngines, "mysql")
				}
				if item.Installed && item.Key == "postgresql-service" {
					v.DBEngines = append(v.DBEngines, "postgresql")
				}
			}
		}
	case "backups":
		v.Error = s.listText(T, ctl.Request{Op: "backup.list", Site: row.Name}, &v.Backups)
		v.Cloud = s.loadCloudText(T, row.Name)
	case "wordpress":
		if row.App != "wordpress" {
			http.NotFound(w, r)
			return
		}
		v.WP = s.loadWP(row, r)
	}
	s.show(w, 200, ss, v)
}

// loadSSH fills in what the SSH/SFTP card of the Overview shows.
func (s *Server) loadSSH(v *view, r *http.Request, row siteRow) {
	v.SSHHost = hostOnly(r.Host)
	v.SSHEnabled = access.Enabled(s.passwdFile(), row.User)
	v.KeyStatus = "unknown"
	if v.SSHEnabled && s.LoginCheck != nil {
		v.KeyStatus = "ok"
		if err := s.LoginCheck(row.User); err != nil {
			v.KeyStatus = "broken"
		}
	}
}

// loginResult runs the helper and, if it worked, adds the account and the new password to what the page shows once.
func (s *Server) loginResult(r *http.Request, title string, row siteRow, password string) *result {
	T := translatorForRequest(r)
	res := s.runText(r, T, title, ctl.Request{Op: "site.access", Site: row.Name, Enable: true, Password: password})
	res.Back = siteTab(row.Name, "sftp")
	if res.OK {
		res.Output += "\n\n" + T("res.access", hostOnly(r.Host), row.User, password)
	}
	return res
}

func (s *Server) sshEnable(w http.ResponseWriter, r *http.Request, ss webauth.Session) {
	T := translatorForWriter(w)
	row, status, msg := s.findSiteText(T, r.PathValue("name"))
	if status != 0 {
		http.Error(w, msg, status)
		return
	}
	password, err := dbadmin.NewPassword()
	if err != nil {
		s.refuseTo(w, ss, siteTab(row.Name, "sftp"), T("err.nothing"), err.Error())
		return
	}
	s.finish(w, r, s.loginResult(r, T("act.ssh.on", row.Name), row, password))
}

var sshActions = map[string]struct{ title, warn, button, act string }{
	"reset":   {"ssh.reset.title", "ssh.reset.warn", "ssh.reset.btn", "act.ssh.reset"},
	"disable": {"ssh.disable.title", "ssh.disable.warn", "ssh.disable.btn", "act.ssh.off"},
}

func (s *Server) sshConfirm(action string) func(http.ResponseWriter, *http.Request, webauth.Session) {
	a := sshActions[action]
	return func(w http.ResponseWriter, r *http.Request, ss webauth.Session) {
		T := translatorForWriter(w)
		row, status, msg := s.findSiteText(T, r.PathValue("name"))
		if status != 0 {
			http.Error(w, msg, status)
			return
		}
		s.show(w, 200, ss, view{Page: "confirm", Confirm: &confirm{Title: T(a.title, row.Name), Warning: T(a.warn), Button: T(a.button),
			Action: "/sites/" + row.Name + "/ssh/" + action, Expect: row.Name, Back: siteTab(row.Name, "sftp")}})
	}
}

func (s *Server) sshAct(action string) func(http.ResponseWriter, *http.Request, webauth.Session) {
	a := sshActions[action]
	return func(w http.ResponseWriter, r *http.Request, ss webauth.Session) {
		T := translatorForWriter(w)
		row, status, msg := s.findSiteText(T, r.PathValue("name"))
		if status != 0 {
			http.Error(w, msg, status)
			return
		}
		if r.PostFormValue("confirm") != row.Name {
			s.refuseTo(w, ss, siteTab(row.Name, "sftp"), T("err.nothing"), T("err.confirm"))
			return
		}
		if action == "disable" {
			res := s.runText(r, T, T(a.act, row.Name), ctl.Request{Op: "site.access", Site: row.Name})
			res.Back = siteTab(row.Name, "sftp")
			s.finish(w, r, res)
			return
		}
		password, err := dbadmin.NewPassword()
		if err != nil {
			s.refuseTo(w, ss, siteTab(row.Name, "sftp"), T("err.nothing"), err.Error())
			return
		}
		s.finish(w, r, s.loginResult(r, T(a.act, row.Name), row, password))
	}
}

func (s *Server) sshPanelKey(w http.ResponseWriter, r *http.Request, ss webauth.Session) {
	T := translatorForWriter(w)
	row, status, msg := s.findSiteText(T, r.PathValue("name"))
	if status != 0 {
		http.Error(w, msg, status)
		return
	}
	res := s.runText(r, T, T("act.ssh.key", row.Name), ctl.Request{Op: "site.panelkey", Site: row.Name})
	res.Back = siteTab(row.Name, "sftp")
	s.finish(w, r, res)
}
