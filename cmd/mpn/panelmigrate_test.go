package main

import (
	"path/filepath"
	"strings"
	"testing"

	"mpn/internal/store"
	"mpn/internal/webauth"
)

func TestRuntimeRemovalPlanKeepsHomeAndDatabaseData(t *testing.T) {
	plan := runtimeRemovalActions([]store.LegacyRuntimeSite{{Name: "legacy-app", User: "legacy-app", Kind: "node"}})
	var raw strings.Builder
	raw.WriteString(plan.String())
	for _, step := range plan {
		for _, action := range step.Actions {
			raw.WriteString(strings.Join(action.Args, " "))
		}
	}
	got := raw.String()
	for _, want := range []string{
		"/etc/systemd/system/app-legacy-app.service",
		"/etc/cron.d/mpn-legacy-app",
		"/etc/nginx/sites-enabled/legacy-app.conf",
		"/run/mpn-app/legacy-app",
		"systemctl daemon-reload",
		"nginx -t",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("plan is missing %q:\n%s", want, got)
		}
	}
	for _, forbidden := range []string{"/home/legacy-app", "userdel", "rm -rf /opt/node", "apt", "dnf"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("plan unexpectedly touches %q:\n%s", forbidden, got)
		}
	}
}

func TestLegacyRuntimeRemovalRevokesDelegatedSiteGrants(t *testing.T) {
	path := filepath.Join(t.TempDir(), "delegated-access.json")
	access := webauth.NewDelegatedAccess()
	access.Users["editor"] = webauth.DelegatedUser{Hash: "hash", Enabled: true, Sites: map[string]map[string]string{
		"0123456789abcdef0123456789abcdef": {"files.read": "allow"},
		"abcdef0123456789abcdef0123456789": {"files.write": "deny"},
	}}
	if err := webauth.SaveDelegatedAccess(path, access); err != nil {
		t.Fatal(err)
	}
	if err := removeLegacySiteGrants(path, []store.LegacyRuntimeSite{{AccessID: "0123456789abcdef0123456789abcdef"}}); err != nil {
		t.Fatal(err)
	}
	got, err := webauth.LoadDelegatedAccess(path)
	if err != nil {
		t.Fatal(err)
	}
	grants := got.Users["editor"].Sites
	if _, ok := grants["0123456789abcdef0123456789abcdef"]; ok {
		t.Fatal("removed site's grants remain")
	}
	if _, ok := grants["abcdef0123456789abcdef0123456789"]; !ok {
		t.Fatal("grant for a remaining site was removed")
	}
}
