package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	for _, path := range []string{"/users", "/firewall", "/services", "/databases/add", "/backups/restore", "/sites/new", "/sites/remove", "/sites/shop/access", "/sites/shop/unknown"} {
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
		firstID: {"site.view": "allow", "files.read": "allow", "files.write": "deny", "ssl.view": "allow", "vhost.view": "allow"},
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
