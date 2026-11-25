package web

import (
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
