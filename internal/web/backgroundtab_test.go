package web

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

func TestBackgroundServiceCreationMatchesWebsiteType(t *testing.T) {
	cases := []struct {
		name, list, kind string
	}{
		{"blog", `[{"name":"blog","kind":"php","user":"blog","hostnames":["blog.example.com"],"detail":"","tls":false,"root":"/home/blog/htdocs"}]`, "php"},
		{"api", `[{"name":"api","kind":"binary","user":"api","hostnames":["api.example.com"],"detail":"","tls":false,"root":"/home/api/app"}]`, "binary"},
		{"demo", `[{"name":"demo","kind":"mise","user":"demo","hostnames":["demo.example.com"],"detail":"","tls":false,"root":"/home/demo/htdocs"}]`, "mise"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			h, _, c, _ := setupWith(t, byOp(map[string]string{
				"site.list":   tc.list,
				"site.bglist": `[]`,
				"mail.status": `{}`,
			}), nil)
			body := do(h, "GET", "/sites/"+tc.name+"/background", nil, cookie(c)).Body.String()
			if !strings.Contains(body, `name="kind" value="`+tc.kind+`"`) {
				t.Fatalf("missing the matching %s service form", tc.kind)
			}
			for _, other := range []string{"php", "binary", "mise"} {
				if other != tc.kind && strings.Contains(body, `name="kind" value="`+other+`"`) {
					t.Errorf("%s site also offers %s service", tc.kind, other)
				}
			}
			if !strings.Contains(body, `<circle cx="10" cy="10" r="3"/>`) {
				t.Error("background tab is missing its icon")
			}
		})
	}
}

func TestStaticAndProxyDoNotOfferBackgroundCreation(t *testing.T) {
	cases := []struct{ name, list string }{
		{"static", `[{"name":"static","kind":"static","user":"static","hostnames":["static.example.com"],"detail":"","tls":false,"root":"/home/static/htdocs"}]`},
		{"proxy", `[{"name":"proxy","kind":"proxy","user":"proxy","hostnames":["proxy.example.com"],"detail":"","tls":false,"root":"/home/proxy/htdocs"}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _, c, _ := setupWith(t, byOp(map[string]string{
				"site.list":   tc.list,
				"site.bglist": `[]`,
				"mail.status": `{}`,
			}), nil)
			body := do(h, "GET", "/sites/"+tc.name+"/background", nil, cookie(c)).Body.String()
			if strings.Contains(body, `name="kind" value=`) {
				t.Error("site without an app runtime offered background-service creation")
			}
		})
	}
}

func TestLegacyMismatchedServiceOnlyOffersSafeActions(t *testing.T) {
	h, _, c, _ := setupWith(t, byOp(map[string]string{
		"site.list":   `[{"name":"demo","kind":"mise","user":"demo","hostnames":["demo.example.com"],"detail":"","tls":false,"root":"/home/demo/htdocs"}]`,
		"site.bglist": `[{"id":9,"site":"demo","name":"old binary worker","kind":"binary","command":"bin/worker","status":"inactive"}]`,
		"mail.status": `{}`,
	}), nil)
	body := do(h, "GET", "/sites/demo/background", nil, cookie(c)).Body.String()
	if strings.Contains(body, "/background/9/start") || strings.Contains(body, `name="action" value="edit"`) {
		t.Fatal("legacy mismatched service still offers start or edit")
	}
	if !strings.Contains(body, "/background/9/delete") {
		t.Fatal("legacy mismatched service cannot be removed")
	}
}

func TestBackgroundServiceActionIcons(t *testing.T) {
	h, _, c, csrf := setupWith(t, byOp(map[string]string{
		"site.list":   `[{"name":"demo","kind":"mise","user":"demo"}]`,
		"site.bglist": `[{"id":7,"name":"queue worker","kind":"mise","status":"active"},{"id":8,"name":"scheduler","kind":"mise","status":"inactive"},{"id":9,"name":"legacy worker","kind":"binary","status":"active"}]`,
	}), nil)
	body := do(h, "GET", "/sites/demo/background", nil, cookie(c), func(r *http.Request) {
		r.Header.Set("Accept-Language", "en")
	}).Body.String()
	for _, tc := range []struct {
		name, id string
		actions  []string
	}{
		{"queue worker", "7", []string{"Recent logs", "Restart", "Stop", "Edit service", "Remove service"}},
		{"scheduler", "8", []string{"Recent logs", "Start", "Edit service", "Remove service"}},
		{"legacy worker", "9", []string{"Recent logs", "Stop", "Remove service"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, card, found := strings.Cut(body, "<h3 class=\"font-semibold\">"+tc.name+"</h3>")
			if !found {
				t.Fatal("service card missing")
			}
			card, _, _ = strings.Cut(card, "</section>")
			for _, label := range []string{"Recent logs", "Restart", "Stop", "Start", "Edit service", "Remove service"} {
				want := slices.Contains(tc.actions, label)
				got := strings.Contains(card, `title="`+label+`" aria-label="`+label+`"><svg`)
				if got != want {
					t.Errorf("%q icon present = %t, want %t", label, got, want)
				}
			}
			row, dialog, found := strings.Cut(card, `<dialog id="dlg-bg-remove-`+tc.id+`"`)
			if !found || !strings.Contains(row, `type="button" class="ico-btn ico-btn-destructive" data-dialog-open="dlg-bg-remove-`+tc.id+`"`) || strings.Contains(row, "/delete") {
				t.Fatal("Remove must open its confirmation dialog without submitting")
			}
			dialog, _, _ = strings.Cut(dialog, "</dialog>")
			for _, want := range []string{
				`method="post" action="/sites/demo/background/` + tc.id + `/delete"`,
				`name="csrf" value="` + csrf + `"`,
				"Remove “" + tc.name + "”?",
				`type="button" class="btn-secondary" data-dialog-close autofocus>Cancel</button>`,
				`type="submit" class="btn-danger">Remove service</button>`,
			} {
				if !strings.Contains(dialog, want) {
					t.Errorf("confirmation dialog missing %s in %s", want, dialog)
				}
			}
		})
	}
}
