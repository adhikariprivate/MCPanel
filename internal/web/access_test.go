package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mpn/internal/ctl"
	"mpn/internal/webauth"
)

func TestRequestCapabilityCoversRegisteredSiteRoutes(t *testing.T) {
	cases := []struct {
		method, path, want, site string
	}{
		{http.MethodGet, "/sites/shop", "site.view", "shop"},
		{http.MethodGet, "/sites/shop/files", "files.read", "shop"},
		{http.MethodGet, "/sites/shop/files/download", "files.read", "shop"},
		{http.MethodPost, "/sites/shop/files/upload", "files.write", "shop"},
		{http.MethodPost, "/sites/shop/files/delete", "files.delete", "shop"},
		{http.MethodPost, "/sites/shop/files/chmod", "files.permissions", "shop"},
		{http.MethodGet, "/sites/shop/terminal/ws", "terminal.open", "shop"},
		{http.MethodPost, "/sites/shop/ssh/panelkey", "ssh.manage", "shop"},
		{http.MethodPost, "/sites/shop/ssl/custom", "ssl.manage", "shop"},
		{http.MethodPost, "/sites/shop/cron/3/delete", "cron.manage", "shop"},
		{http.MethodGet, "/sites/shop/background", "background.view", "shop"},
		{http.MethodPost, "/sites/shop/background", "background.manage", "shop"},
		{http.MethodPost, "/sites/shop/background/3/restart", "background.manage", "shop"},
		{http.MethodGet, "/sites/shop/advanced", "site.advanced", "shop"},
		{http.MethodPost, "/sites/shop/vhost/reset", "vhost.manage", "shop"},
		{http.MethodGet, "/sites/shop/email/cf", "email.view", "shop"},
		{http.MethodPost, "/sites/shop/wp/run", "wordpress.manage", "shop"},
		{http.MethodGet, "/sites/shop/cloud/backups", "cloud.view", "shop"},
		{http.MethodPost, "/sites/shop/cloud/enable", "cloud.manage", "shop"},
		{http.MethodPost, "/sites/shop/cloud/sync", "cloud.manage", "shop"},
		{http.MethodPost, "/sites/shop/cloud/fetch", "cloud.manage", "shop"},
		{http.MethodGet, "/activity", "activity.view", ""},
		{http.MethodGet, "/activity.csv", "activity.view", ""},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		got, site, ownerOnly := requestCapability(r)
		if ownerOnly || got != tc.want || site != tc.site {
			t.Errorf("%s %s: got capability=%q site=%q ownerOnly=%t; want %q for %q", tc.method, tc.path, got, site, ownerOnly, tc.want, tc.site)
		}
	}
	for _, path := range []string{"/users", "/firewall", "/services", "/databases/add", "/backups/restore", "/backups/offsite", "/cloud/connect", "/cloud/configure", "/cloud/finish", "/cloud/disconnect", "/sites/shop/cloud/connect", "/sites/shop/cloud/configure", "/sites/shop/cloud/finish", "/sites/shop/cloud/disconnect", "/sites/new", "/sites/remove", "/sites/shop/access", "/sites/shop/unknown"} {
		r := httptest.NewRequest(http.MethodPost, path, nil)
		if capability, _, ownerOnly := requestCapability(r); !ownerOnly && capability != "session.logout" && capability != "session.result" {
			t.Errorf("%s: expected owner-only or session-only route, got capability %q", path, capability)
		}
	}
	for _, path := range []string{"/logout", "/result"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if capability, _, _ := requestCapability(r); capability == "" {
			t.Errorf("%s: delegated session route should be recognized", path)
		}
	}
}

func TestDelegatedAuthorizationIsPerSiteAndFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "delegated.json")
	access := webauth.NewDelegatedAccess()
	const firstID = "00000000000000000000000000000001"
	siteJSON := `[{"id":1,"access_id":"` + firstID + `","name":"shop"}]`
	access.Users["editor"] = webauth.NewDelegatedUser("stored-hash", map[string]map[string]string{
		firstID: {"site.view": "allow", "files.read": "allow", "files.write": "deny", "ssl.view": "allow", "vhost.view": "allow", "cloud.view": "allow", "cloud.manage": "allow"},
	})
	if err := webauth.SaveDelegatedAccess(path, access); err != nil {
		t.Fatal(err)
	}
	owner := account()
	s := &Server{
		Account:    func() (webauth.Account, error) { return owner, nil },
		AccessFile: path,
		Sessions:   webauth.NewSessions(time.Minute, time.Hour, 10),
		Call: func(ctl.Request) (ctl.Response, error) {
			return ctl.Response{OK: true, Output: siteJSON}, nil
		},
	}
	for _, tc := range []struct {
		method, target string
		want           bool
	}{
		{http.MethodGet, "/sites/shop", true},
		{http.MethodGet, "/sites/shop/files", true},
		{http.MethodPost, "/sites/shop/files/upload", false},
		{http.MethodGet, "/sites/shop/domain", true},
		{http.MethodGet, "/sites/shop/advanced", true},
		{http.MethodGet, "/activity", true},
		{http.MethodGet, "/activity.csv", true},
		{http.MethodGet, "/sites/shop/cloud/backups", true},
		{http.MethodPost, "/sites/shop/cloud/enable", true},
		{http.MethodPost, "/sites/shop/cloud/sync", true},
		{http.MethodPost, "/sites/shop/cloud/fetch", true},
		{http.MethodGet, "/sites/other/cloud/backups", false},
		{http.MethodPost, "/sites/other/cloud/sync", false},
		{http.MethodGet, "/sites/other", false},
		{http.MethodGet, "/services", false},
		{http.MethodGet, "/users", false},
		{http.MethodGet, "/result", true},
		{http.MethodPost, "/logout", true},
	} {
		r := httptest.NewRequest(tc.method, tc.target, nil)
		w := httptest.NewRecorder()
		got := s.authorizeRequest(w, r, webauth.Session{User: "editor"})
		if got != tc.want {
			t.Errorf("%s %s: authorize=%t, want %t (status %d)", tc.method, tc.target, got, tc.want, w.Code)
		}
	}
	for _, tc := range []struct{ method, target string }{
		{http.MethodGet, "/backups/offsite"},
		{http.MethodPost, "/cloud/configure"},
		{http.MethodPost, "/cloud/connect"},
		{http.MethodPost, "/cloud/finish"},
		{http.MethodPost, "/cloud/disconnect"},
		{http.MethodPost, "/sites/shop/cloud/configure"},
		{http.MethodPost, "/sites/shop/cloud/connect"},
		{http.MethodPost, "/sites/shop/cloud/finish"},
		{http.MethodPost, "/sites/shop/cloud/disconnect"},
	} {
		r := httptest.NewRequest(tc.method, tc.target, nil)
		w := httptest.NewRecorder()
		if s.authorizeRequest(w, r, webauth.Session{User: "editor"}) || w.Code != http.StatusForbidden {
			t.Errorf("delegated user: %s %s returned %d, want %d", tc.method, tc.target, w.Code, http.StatusForbidden)
		}
		if !s.authorizeRequest(httptest.NewRecorder(), r, webauth.Session{User: owner.User}) {
			t.Errorf("instance administrator was denied %s %s", tc.method, tc.target)
		}
	}
	siteJSON = `[{"id":1,"access_id":"00000000000000000000000000000002","name":"shop"}]`
	w := httptest.NewRecorder()
	if s.authorizeRequest(w, httptest.NewRequest(http.MethodGet, "/sites/shop/files", nil), webauth.Session{User: "editor"}) {
		t.Fatal("recreated site identity must not inherit the deleted site's grants")
	}
}

func TestBackgroundPermissionCanBeSavedAndLoaded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "delegated.json")
	access := webauth.NewDelegatedAccess()
	const siteID = "00000000000000000000000000000001"
	access.Users["editor"] = webauth.NewDelegatedUser("stored-hash", map[string]map[string]string{
		siteID: {"site.view": "allow", "background.view": "allow", "background.manage": "allow"},
	})
	if err := webauth.SaveDelegatedAccess(path, access); err != nil {
		t.Fatalf("save user access with background permissions: %v", err)
	}
	loaded, err := webauth.LoadDelegatedAccess(path)
	if err != nil {
		t.Fatalf("load user access with background permissions: %v", err)
	}
	if !loaded.Allows("editor", siteID, "background.view") || !loaded.Allows("editor", siteID, "background.manage") {
		t.Fatal("saved background permissions were not retained")
	}
}

func TestDelegatedSessionRevocationRemovesUserSessions(t *testing.T) {
	sessions := webauth.NewSessions(time.Minute, time.Hour, 10)
	editorToken, _ := sessions.Create("editor")
	adminToken, _ := sessions.Create("admin")
	sessions.DeleteUser("editor")
	if _, ok := sessions.Get(editorToken); ok {
		t.Fatal("editor session remains active after revocation")
	}
	if got, ok := sessions.Get(adminToken); !ok || got.User != "admin" {
		t.Fatal("revoking editor sessions also removed the admin session")
	}
}

func TestAccessPresetsAreStartingSetsAndKeepSensitiveShellAccessSeparate(t *testing.T) {
	for _, preset := range []string{"read-only", "developer", "site-manager"} {
		if len(accessPreset(preset)) == 0 {
			t.Fatalf("preset %q is empty", preset)
		}
	}
	for _, preset := range []string{"read-only", "developer", "site-manager"} {
		for _, capability := range accessPreset(preset) {
			if capability == "terminal.open" || capability == "ssh.manage" {
				t.Errorf("preset %q unexpectedly grants sensitive shell capability %q", preset, capability)
			}
		}
	}
	if got := accessPreset("unknown"); len(got) != 0 {
		t.Errorf("unknown preset grants capabilities: %v", got)
	}
}

func TestReadOnlyBackgroundServicesOfferLogsOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "delegated.json")
	const siteID = "00000000000000000000000000000001"
	access := webauth.NewDelegatedAccess()
	access.Users["reader"] = webauth.NewDelegatedUser("stored-hash", map[string]map[string]string{
		siteID: {"site.view": "allow", "background.view": "allow"},
	})
	if err := webauth.SaveDelegatedAccess(path, access); err != nil {
		t.Fatal(err)
	}
	s := newServer()
	s.AccessFile = path
	s.Call = byOp(map[string]string{
		"site.list":   `[{"id":1,"access_id":"` + siteID + `","name":"demo","kind":"mise","user":"demo"}]`,
		"site.bglist": `[{"id":7,"name":"queue worker","kind":"mise","status":"active"},{"id":8,"name":"scheduler","kind":"mise","status":"inactive"}]`,
	})
	token, _ := s.Sessions.Create("reader")
	h := s.Handler()
	ck := cookie(&http.Cookie{Name: "mpn", Value: token})
	w := do(h, http.MethodGet, "/sites/demo/background", nil, ck)
	if w.Code != http.StatusOK {
		t.Fatalf("background services status = %d", w.Code)
	}
	body := w.Body.String()
	for _, id := range []string{"7", "8"} {
		if !strings.Contains(body, `href="/sites/demo/background?logs=`+id+`" title="Nhật ký gần đây" aria-label="Nhật ký gần đây"`) {
			t.Errorf("service %s has no accessible Recent logs link", id)
		}
	}
	for _, forbidden := range []string{`method="post" action="/sites/demo/background`, "dlg-bg-edit-", "dlg-bg-remove-", "dlg-bg-add"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("read-only user sees management control %s", forbidden)
		}
	}
}
