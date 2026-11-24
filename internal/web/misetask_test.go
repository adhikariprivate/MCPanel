package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const miseSiteList = `[{"name":"demo","kind":"mise","user":"demo","hostnames":["demo.example.com"],"detail":"mise 20012","tls":false,"root":"/home/demo/htdocs"}]`

func miseInfo(task, taskError string) string {
	return fmt.Sprintf(`{"name":"demo","kind":"mise","user":"demo","hostnames":["demo.example.com"],"root":"/home/demo/htdocs","port":20012,"mem_mb":320,"mise_task":%q,"mise_tasks":[{"name":"serve","description":"Web server"}],"mise_error":%q}`, task, taskError)
}

func TestMiseTaskChooserShowsActiveStateAccountAndCommandPreview(t *testing.T) {
	h, _, c, _ := setupWith(t, byOp(map[string]string{"site.list": miseSiteList, "site.info": miseInfo("serve", "")}), nil)
	english := cookie(&http.Cookie{Name: "mpn_language", Value: "en"})
	body := do(h, "GET", "/sites/demo", nil, cookie(c), english).Body.String()
	for _, want := range []string{
		`action="/sites/demo/mise-task"`, `data-mise-task-form`, "Active", "mise run serve", "demo", "Save &amp; reload service", `data-mise-discard`, `mise.js?v=`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("mise task card missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `action="/sites/demo/settings"`) {
		t.Error("mise task card must not use the combined site settings form")
	}
}

func TestMiseTaskSaveUsesExistingAllowlistAndPreservesPortAndMemory(t *testing.T) {
	answers := map[string]string{"site.list": miseSiteList, "site.info": miseInfo("", ""), "site.set": "Mise service restarted"}
	h, f, c, csrf := setupWith(t, byOp(answers), nil)
	w := do(h, "POST", "/sites/demo/mise-task", url.Values{"csrf": {csrf}, "mise_task": {"serve"}}, cookie(c))
	if w.Code != 303 || w.Header().Get("Location") != "/result" {
		t.Fatalf("save status=%d location=%q", w.Code, w.Header().Get("Location"))
	}
	req, ok := lastOp(f, "site.set")
	if !ok || req.MiseTask != "serve" || req.Port != 20012 || req.Mem != 320 || req.Site != "demo" {
		t.Fatalf("site.set request did not preserve the current Mise service settings: %+v, present=%t", req, ok)
	}
}

func TestMiseTaskSaveAllowsStoppedWithUnreadableTaskListButRejectsUnlistedTasks(t *testing.T) {
	for _, tc := range []struct {
		name, selected, taskError string
		wantSet                   bool
	}{
		{"stop even when mise.toml cannot be read", "", "permission denied", true},
		{"reject a task absent from mise.toml", "other", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			answers := map[string]string{"site.list": miseSiteList, "site.info": miseInfo("serve", tc.taskError), "site.set": "saved"}
			h, f, c, csrf := setupWith(t, byOp(answers), nil)
			do(h, "POST", "/sites/demo/mise-task", url.Values{"csrf": {csrf}, "mise_task": {tc.selected}}, cookie(c))
			_, got := lastOp(f, "site.set")
			if got != tc.wantSet {
				t.Errorf("site.set called=%t, want %t; calls: %v", got, tc.wantSet, f.ops())
			}
		})
	}
}

func TestMiseTaskRouteRequiresSiteSettingsCapability(t *testing.T) {
	r := httptest.NewRequest("POST", "/sites/demo/mise-task", nil)
	got, site, ownerOnly := requestCapability(r)
	if ownerOnly || got != "site.settings" || site != "demo" {
		t.Fatalf("route permission = %q site=%q ownerOnly=%t", got, site, ownerOnly)
	}
}

func TestGenericSettingsRequestPreservesTheCurrentMiseTask(t *testing.T) {
	r := httptest.NewRequest("POST", "/sites/demo/settings", strings.NewReader(""))
	request, problem := (&Server{}).settingsRequest(r, siteRow{Name: "demo", Kind: "mise"}, siteInfo{MiseTask: "serve", Port: 20012, MemMB: 320})
	if problem != "" {
		t.Fatalf("unexpected validation problem: %s", problem)
	}
	if request.MiseTask != "serve" || request.Port != 20012 || request.Mem != 320 {
		t.Fatalf("generic settings request should preserve the Mise task and service settings: %+v", request)
	}
}
