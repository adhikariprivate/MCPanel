package web

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

var textKeyRe = regexp.MustCompile(`\{\{\s*T\s+"([^"]+)"`)

func TestEveryTextKeyInTheTemplatesHasATranslation(t *testing.T) {
	entries, err := fs.ReadDir(templateFS, "templates")
	if err != nil || len(entries) == 0 {
		t.Fatalf("no templates: %v", err)
	}
	for _, e := range entries {
		b, _ := templateFS.ReadFile("templates/" + e.Name())
		for _, m := range textKeyRe.FindAllStringSubmatch(string(b), -1) {
			if _, ok := vi[m[1]]; !ok {
				t.Errorf("%s uses the text key %q, which has no translation", e.Name(), m[1])
			}
		}
	}
	for name := range pages {
		if _, ok := vi["page."+name]; !ok {
			t.Errorf("page %q has no title", name)
		}
	}
}

func TestLoginPageUsesTheNewDesignInVietnamese(t *testing.T) {
	body := do(newServer().Handler(), "GET", "/login", nil).Body.String()
	for _, want := range []string{`lang="vi"`, `name="password"`, "Đăng nhập", "Tên đăng nhập", "data-menu-button", `href="/language/en"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestAuthenticatedShellUsesSidebarAndAccountMenu(t *testing.T) {
	h, _, c, _ := setup(t, okAnswer(""))
	body := do(h, "GET", "/", nil, cookie(c)).Body.String()
	for _, want := range []string{`class="app-sidebar"`, `class="app-content"`, `aria-label="Điều hướng chính"`,
		`href="/" class="nav-link" aria-current="page"`, `href="/email" class="nav-link"`, `href="/monitor" class="nav-link"`,
		`href="/firewall" class="nav-link"`, `href="/services" class="nav-link"`, `href="/security" class="nav-link"`,
		`href="/activity" class="nav-link"`, `href="/notify" class="nav-link"`, `href="/update" class="nav-link"`,
		`data-theme-toggle`, `href="/settings"`, `href="/language/en"`, `action="/logout"`} {
		if !strings.Contains(body, want) {
			t.Errorf("sidebar shell missing %q", want)
		}
	}
	if strings.Contains(body, `class="app-nav"`) {
		t.Error("authenticated shell still has the horizontal main navigation")
	}
}

func TestLoginMessagesAreInVietnamese(t *testing.T) {
	h := newServer().Handler()
	if body := login(h, "sai-mat-khau").Body.String(); !strings.Contains(body, "Sai tên đăng nhập hoặc mật khẩu.") {
		t.Errorf("wrong-login message:\n%s", body)
	}
	for i := 0; i < 5; i++ {
		login(h, "sai-mat-khau")
	}
	if body := login(h, goodPW).Body.String(); !strings.Contains(body, "Hãy thử lại sau") {
		t.Errorf("lock message:\n%s", body)
	}
}

func TestStaticAssetsAreServedAndTheStylesheetIsCurrent(t *testing.T) {
	h := newServer().Handler()
	css := do(h, "GET", "/static/app.css", nil)
	if css.Code != 200 || !strings.HasPrefix(css.Header().Get("Content-Type"), "text/css") || css.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("css: %d %q %q", css.Code, css.Header().Get("Content-Type"), css.Header().Get("Cache-Control"))
	}
	// A stylesheet built before the last template change would miss these classes: run the Tailwind build again.
	for _, want := range []string{".btn-primary", ".card", ".field", ".app-shell", ".bg-primary", ".app-sidebar", ".account-panel", ".site-tabs", ".accent-primary", "JetBrains Mono"} {
		if !strings.Contains(css.Body.String(), want) {
			t.Errorf("the stylesheet lacks %s", want)
		}
	}
	for _, p := range []string{"/static/app.js", "/static/theme.js"} {
		if w := do(h, "GET", p, nil); w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/javascript") {
			t.Errorf("%s: %d %q", p, w.Code, w.Header().Get("Content-Type"))
		}
	}
}

func TestAssetsAreVersionedSoABrowserNeverRunsAStaleScript(t *testing.T) {
	h, _, c, _ := setup(t, okAnswer(""))
	body := do(h, "GET", "/", nil, cookie(c)).Body.String()
	for _, name := range []string{"app.css", "app.js", "theme.js"} {
		url := "/static/" + name + "?v=" + assetVersion[name]
		if !strings.Contains(body, url) {
			t.Errorf("the main app page must link %s", url)
		}
		w := do(h, "GET", url, nil)
		if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
			t.Errorf("%s: %d %q", url, w.Code, w.Header().Get("Cache-Control"))
		}
		if w2 := do(h, "GET", "/static/"+name, nil, header("If-None-Match", w.Header().Get("ETag"))); w2.Code != 304 {
			t.Errorf("%s: an unchanged file must answer 304, got %d", name, w2.Code)
		}
	}
	if assetVersion["app.js"] == assetVersion["theme.js"] || len(assetVersion["app.css"]) != 8 {
		t.Errorf("versions: %v", assetVersion)
	}
}

func TestThePasswordIsFilledInEvenWithoutScript(t *testing.T) {
	h, _, c, _ := setup(t, okAnswer(""))
	re := regexp.MustCompile(`name="password"[^>]*value="([A-Za-z0-9]{24})"`)
	a := re.FindStringSubmatch(do(h, "GET", "/sites/new?kind=php", nil, cookie(c)).Body.String())
	b := re.FindStringSubmatch(do(h, "GET", "/sites/new?kind=php", nil, cookie(c)).Body.String())
	if a == nil || b == nil || a[1] == b[1] {
		t.Errorf("each form must come with its own random password: %v %v", a, b)
	}
}
