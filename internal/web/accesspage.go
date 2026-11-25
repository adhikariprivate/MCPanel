package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"mpn/internal/ctl"
	"mpn/internal/webauth"
)

var panelUserNameRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{2,31}$`)

type accessCapabilityView struct {
	Key, Label, Effect string
}

type accessCapabilityGroupView struct {
	Name         string
	Capabilities []accessCapabilityView
}

type accessSiteView struct {
	Name, ID string
	Assigned bool
	Groups   []accessCapabilityGroupView
}

type accessUserRow struct {
	Name      string
	Enabled   bool
	SiteCount int
}

type accessPersonView struct {
	Name, State, Summary string
}

func (s *Server) accessPages(mux *http.ServeMux) {
	mux.HandleFunc("GET /users", s.auth(s.usersPage))
	mux.HandleFunc("POST /users/save", s.auth(s.savePanelUser))
}

func (s *Server) loadSiteRows() ([]siteRow, error) {
	resp, err := s.ask(ctl.Request{Op: "site.list"})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, errors.New(resp.Error)
	}
	var rows []siteRow
	if err := json.Unmarshal([]byte(resp.Output), &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Server) usersPage(w http.ResponseWriter, r *http.Request, ss webauth.Session) {
	access, err := s.delegatedAccess()
	if err != nil {
		http.Error(w, "access control unavailable", http.StatusServiceUnavailable)
		return
	}
	rows, err := s.loadSiteRows()
	if err != nil {
		http.Error(w, "could not load websites", http.StatusBadGateway)
		return
	}
	v := view{Page: "users", AccessUsername: r.URL.Query().Get("user"), AccessEditing: r.URL.Query().Get("user") != ""}
	for name, u := range access.Users {
		v.AccessUsers = append(v.AccessUsers, accessUserRow{Name: name, Enabled: u.Enabled, SiteCount: len(u.Sites)})
	}
	slicesSortUsers(v.AccessUsers)
	if v.AccessEditing {
		u, ok := access.Users[v.AccessUsername]
		if !ok {
			http.NotFound(w, r)
			return
		}
		v.AccessEnabled = u.Enabled
	}
	for _, row := range rows {
		siteID := row.AccessID
		item := accessSiteView{Name: row.Name, ID: siteID}
		var grants map[string]string
		if v.AccessEditing {
			var ok bool
			grants, ok = access.Users[v.AccessUsername].Sites[siteID]
			item.Assigned = ok
		}
		item.Groups = accessPermissionGroups(grants)
		v.AccessSites = append(v.AccessSites, item)
	}
	s.show(w, http.StatusOK, ss, v)
}

var accessCapabilities = []string{
	"site.settings", "vhost.view", "vhost.manage", "files.read", "files.write", "files.delete", "files.permissions",
	"ssl.view", "ssl.manage", "cron.view", "cron.manage", "background.view", "background.manage", "logs.view", "database.view", "backup.view",
	"email.view", "email.manage", "terminal.open", "ssh.manage", "wordpress.view", "wordpress.manage", "cloud.view", "cloud.manage", "app.manage",
}

var accessCapabilityGroupKeys = []struct {
	Name string
	Keys []string
}{
	{"Website", []string{"site.settings", "vhost.view", "vhost.manage"}},
	{"Files", []string{"files.read", "files.write", "files.delete", "files.permissions"}},
	{"Security and access", []string{"ssl.view", "ssl.manage", "terminal.open", "ssh.manage"}},
	{"Scheduled work and logs", []string{"cron.view", "cron.manage", "background.view", "background.manage", "logs.view"}},
	{"Data", []string{"database.view", "backup.view"}},
	{"Email", []string{"email.view", "email.manage"}},
	{"Applications and cloud backups", []string{"wordpress.view", "wordpress.manage", "app.manage", "cloud.view", "cloud.manage"}},
}

var accessCapabilityLabels = map[string]string{
	"site.settings": "Change site settings", "vhost.view": "View vHost", "vhost.manage": "Edit vHost",
	"files.read": "Browse and download files", "files.write": "Create and edit files", "files.delete": "Delete files", "files.permissions": "Change file permissions",
	"ssl.view": "View SSL certificates", "ssl.manage": "Manage SSL certificates", "terminal.open": "Open web terminal", "ssh.manage": "Manage SSH and SFTP",
	"cron.view": "View scheduled jobs", "cron.manage": "Manage scheduled jobs", "background.view": "View background services", "background.manage": "Manage background services", "logs.view": "View site logs",
	"database.view": "List databases", "backup.view": "List backups", "email.view": "View site email", "email.manage": "Manage site email",
	"wordpress.view": "View WordPress", "wordpress.manage": "Manage WordPress", "app.manage": "Install or change the application",
	"cloud.view": "View cloud backups", "cloud.manage": "Manage cloud backups",
}

func accessPermissionGroups(grants map[string]string) []accessCapabilityGroupView {
	groups := make([]accessCapabilityGroupView, 0, len(accessCapabilityGroupKeys))
	for _, group := range accessCapabilityGroupKeys {
		view := accessCapabilityGroupView{Name: group.Name}
		for _, key := range group.Keys {
			view.Capabilities = append(view.Capabilities, accessCapabilityView{Key: key, Label: accessCapabilityLabels[key], Effect: grants[key]})
		}
		groups = append(groups, view)
	}
	return groups
}

// accessPreset returns the capabilities offered by a role preset. Presets are only starting points; the owner can
// change any permission on each site before saving the account.
func accessPreset(name string) []string {
	switch name {
	case "read-only":
		return []string{"files.read", "vhost.view", "ssl.view", "cron.view", "background.view", "logs.view", "database.view", "backup.view", "email.view", "wordpress.view", "cloud.view"}
	case "developer":
		return []string{"site.settings", "files.read", "files.write", "vhost.view", "ssl.view", "cron.view", "cron.manage", "background.view", "background.manage", "logs.view", "database.view", "backup.view", "email.view", "wordpress.view"}
	case "site-manager":
		return []string{"site.settings", "vhost.view", "vhost.manage", "files.read", "files.write", "files.delete", "files.permissions", "ssl.view", "ssl.manage", "cron.view", "cron.manage", "background.view", "background.manage", "logs.view", "database.view", "backup.view", "email.view", "email.manage", "wordpress.view", "wordpress.manage", "cloud.view", "cloud.manage", "app.manage"}
	default:
		return nil
	}
}

func (s *Server) revokeSiteAccess(r *http.Request, siteID, siteName, actor string) {
	if siteID == "" || s.AccessFile == "" {
		return
	}
	s.accessMu.Lock()
	defer s.accessMu.Unlock()
	access, err := webauth.LoadDelegatedAccess(s.AccessFile)
	if err != nil {
		return
	}
	var changed []string
	for name, user := range access.Users {
		if _, ok := user.Sites[siteID]; !ok {
			continue
		}
		delete(user.Sites, siteID)
		user.Updated = time.Now().Unix()
		access.Users[name] = user
		changed = append(changed, name)
	}
	if len(changed) == 0 || webauth.SaveDelegatedAccess(s.AccessFile, access) != nil {
		return
	}
	for _, name := range changed {
		s.note(r, "account.user_update", siteName, fmt.Sprintf("by=%s user=%s removed_site_assignment=true", actor, name), false)
	}
}

func slicesSortUsers(users []accessUserRow) {
	for i := 1; i < len(users); i++ {
		for j := i; j > 0 && users[j].Name < users[j-1].Name; j-- {
			users[j], users[j-1] = users[j-1], users[j]
		}
	}
}

func (s *Server) savePanelUser(w http.ResponseWriter, r *http.Request, ss webauth.Session) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.PostFormValue("username"))
	if !panelUserNameRE.MatchString(name) || s.isOwner(name) {
		http.Error(w, "invalid or reserved panel username", http.StatusBadRequest)
		return
	}
	account, err := s.Account()
	if err != nil {
		http.Error(w, "owner account unavailable", http.StatusServiceUnavailable)
		return
	}
	if name == account.User {
		http.Error(w, "the instance administrator cannot be delegated", http.StatusBadRequest)
		return
	}
	rows, err := s.loadSiteRows()
	if err != nil {
		http.Error(w, "could not load websites", http.StatusBadGateway)
		return
	}
	s.accessMu.Lock()
	defer s.accessMu.Unlock()
	access, err := webauth.LoadDelegatedAccess(s.AccessFile)
	if err != nil {
		http.Error(w, "access control unavailable", http.StatusServiceUnavailable)
		return
	}
	old, exists := access.Users[name]
	hash := old.Hash
	pw := r.PostFormValue("password")
	if !exists || pw != "" {
		if len(pw) < webauth.MinPasswordLen || len(pw) > 256 {
			http.Error(w, "password must be at least 12 characters and no more than 256", http.StatusBadRequest)
			return
		}
		hash, err = webauth.Hash(pw)
		if err != nil {
			http.Error(w, "could not save password", http.StatusInternalServerError)
			return
		}
	}
	sites := map[string]map[string]string{}
	grantCount := 0
	var preset []string
	if !exists {
		preset = accessPreset(r.PostFormValue("preset"))
	}
	for _, row := range rows {
		siteID := row.AccessID
		if r.PostFormValue("site:"+siteID) == "" {
			continue
		}
		grants := map[string]string{"site.view": "allow"} // assigned sites always have a usable landing page
		grantCount++
		for _, capability := range preset {
			grants[capability] = "allow"
			grantCount++
		}
		for _, capability := range accessCapabilities {
			value := r.PostFormValue("grant:" + siteID + ":" + capability)
			if value == "allow" || value == "deny" {
				grants[capability] = value
				grantCount++
			}
		}
		sites[siteID] = grants
	}
	enabled := r.PostFormValue("enabled") == "on"
	if !exists {
		enabled = true
	}
	access.Users[name] = webauth.NewDelegatedUser(hash, sites)
	user := access.Users[name]
	user.Enabled = enabled
	access.Users[name] = user
	if err := webauth.SaveDelegatedAccess(s.AccessFile, access); err != nil {
		http.Error(w, "could not save user access", http.StatusInternalServerError)
		return
	}
	// Access is looked up from the sidecar on each request. Also terminate every old session so a reduction takes
	// effect immediately in browsers holding a live session cookie.
	if s.Sessions != nil {
		s.Sessions.DeleteUser(name)
	}
	event := "account.user_update"
	if !exists {
		event = "account.user_create"
	}
	s.note(r, event, "", fmt.Sprintf("by=%s user=%s sites=%d grants=%d enabled=%t", ss.User, name, len(sites), grantCount, enabled), false)
	http.Redirect(w, r, "/users?user="+name, http.StatusSeeOther)
}

func (s *Server) siteAccessPage(w http.ResponseWriter, r *http.Request, ss webauth.Session) {
	row, status, msg := s.findSiteText(translatorForWriter(w), r.PathValue("name"))
	if status != 0 {
		http.Error(w, msg, status)
		return
	}
	access, err := s.delegatedAccess()
	if err != nil {
		http.Error(w, "access control unavailable", http.StatusServiceUnavailable)
		return
	}
	tabAccess, emailEnabled := s.siteNavState(translatorForWriter(w), ss.User, row)
	v := view{Page: "siteaccess", Site: row, Tab: "access", TabAccess: tabAccess, EmailEnabled: emailEnabled, AccessSite: row.Name}
	for name, user := range access.Users {
		grants, ok := user.Sites[row.AccessID]
		if !ok {
			continue
		}
		state := "Disabled"
		if user.Enabled {
			state = "Enabled"
		}
		var allowed []string
		for cap, effect := range grants {
			if effect == "allow" {
				allowed = append(allowed, cap)
			}
		}
		v.AccessPeople = append(v.AccessPeople, accessPersonView{Name: name, State: state, Summary: strings.Join(allowed, ", ")})
	}
	for i := 1; i < len(v.AccessPeople); i++ {
		for j := i; j > 0 && v.AccessPeople[j].Name < v.AccessPeople[j-1].Name; j-- {
			v.AccessPeople[j], v.AccessPeople[j-1] = v.AccessPeople[j-1], v.AccessPeople[j]
		}
	}
	s.show(w, http.StatusOK, ss, v)
}
