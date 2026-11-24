package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"mpn/internal/ctl"
)

func byOp(answers map[string]string) func(ctl.Request) (ctl.Response, error) {
	return func(r ctl.Request) (ctl.Response, error) {
		out, ok := answers[r.Op]
		if !ok {
			return ctl.Response{Error: "unexpected " + r.Op}, nil
		}
		return ctl.Response{OK: true, Output: out}, nil
	}
}

func TestDatabasesTabListsDatabasesSafely(t *testing.T) {
	h, _, c, _ := setup(t, byOp(map[string]string{
		"db.list":   `[{"name":"blog","site":"blog"},{"name":"x<i>","site":"blog"}]`,
		"site.list": `[{"name":"blog","kind":"php","user":"blog","hostnames":["b.example.com"],"detail":"","tls":false}]`,
	}))
	body := do(h, "GET", "/sites/blog/databases", nil, cookie(c)).Body.String()
	for _, want := range []string{"/databases/passwd?name=blog", "/databases/remove?name=blog", "x&lt;i&gt;"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<i>") {
		t.Error("a database name must be escaped")
	}
}

func TestDatabasesTabShowsAHelperFailure(t *testing.T) {
	h, _, c, _ := setup(t, func(ctl.Request) (ctl.Response, error) {
		return ctl.Response{}, errors.New("the root helper is not reachable")
	})
	w := do(h, "GET", "/sites/blog/databases", nil, cookie(c))
	if w.Code != 502 || !strings.Contains(w.Body.String(), "not reachable") {
		t.Errorf("%d\n%s", w.Code, w.Body.String())
	}
}

func TestTheDatabaseNameIsMadeFromWhatWasTyped(t *testing.T) {
	long := strings.Repeat("x", 30)
	for _, c := range []struct{ site, typed, want, problem string }{
		{"a-com", "", "", ""},                             // nothing typed: the helper picks a_com
		{"a-com", "a", "a_com_a", ""},                     // what failed before: "a" is the end of the name
		{"a-com", "  Shop ", "a_com_shop", ""},            // spaces and capitals are not the person's concern
		{"a-com", "a_com", "a_com", ""},                   // the full default name is kept
		{"a-com", "a-com-shop", "a_com_shop", ""},         // the full name with dashes is kept
		{"a-com", "a_com_shop", "a_com_shop", ""},         // already the right start
		{"blog", "blog", "blog", ""},                      // a site whose name has no dash
		{"blog", "blogger", "blog_blogger", ""},           // starts with the site name but not with "blog_": an ending
		{"a-com", "shop!", "", "chữ cái a-z"},             // characters the database server does not take
		{"a-com", "x'; DROP", "", "chữ cái a-z"},          // never reaches the helper
		{"a-com", "Mật-khẩu", "", "chữ cái a-z"},          // accents
		{"a-com", "../x", "", "chữ cái a-z"},              // path characters
		{"a-com", long, "", "tối đa 32 ký tự"},            // too long once the start is added
		{strings.Repeat("s", 33), "x", "", "Tên website"}, // the site name alone does not fit
	} {
		got, problem := dbNameFor(c.site, c.typed)
		if got != c.want || (c.problem == "") != (problem == "") || (c.problem != "" && !strings.Contains(problem, c.problem)) {
			t.Errorf("dbNameFor(%q, %q) = %q, %q; want %q, problem containing %q", c.site, c.typed, got, problem, c.want, c.problem)
		}
	}
}

func TestAddingADatabaseWithAShortNameWorksAndABadOneIsExplained(t *testing.T) {
	h, f, c, tok := setup(t, okAnswer("database created\n  password: S3cretPw"))
	do(h, "POST", "/databases/add", url.Values{"csrf": {tok}, "site": {"blog"}, "name": {"a"}}, cookie(c))
	if len(f.reqs) != 1 || f.reqs[0].Name != "blog_a" {
		t.Fatalf("a short name must become the end of the name: %+v", f.reqs)
	}
	h, f, c, tok = setup(t, okAnswer("done"))
	w := do(h, "POST", "/databases/add", url.Values{"csrf": {tok}, "site": {"blog"}, "name": {"bad name!"}}, cookie(c))
	if w.Code != 400 || len(f.reqs) != 0 {
		t.Fatalf("a bad name must be refused here: %d, %d requests", w.Code, len(f.reqs))
	}
	if body := w.Body.String(); !strings.Contains(body, "chữ cái a-z") || strings.Contains(body, "is not named") {
		t.Errorf("the explanation must be in Vietnamese:\n%s", body)
	}
}

func TestTheSiteTabShowsTheFixedStartOfTheName(t *testing.T) {
	h, _, c, _ := setupWith(t, sitePageAnswers(), nil)
	body := do(h, "GET", "/sites/blog/databases", nil, cookie(c)).Body.String()
	for _, want := range []string{`>blog_</span>`, `placeholder="phần đuôi, ví dụ: shop"`, "gõ shop sẽ thành blog_shop"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, `pattern="[a-z0-9_]{1,32}"`) {
		t.Error("the old pattern would have refused a capital letter before the server could tidy it")
	}
}

func TestDatabaseEngineSelectorUsesInstalledEnginesAndLabelsRows(t *testing.T) {
	h, _, c, _ := setupWith(t, sitePageAnswers(), nil)
	body := do(h, "GET", "/sites/blog/databases", nil, cookie(c), cookie(&http.Cookie{Name: "mpn_language", Value: "en"})).Body.String()
	for _, want := range []string{`value="mysql"`, `value="postgresql"`, `>PostgreSQL</option>`, `>Engine</th>`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestAddingADatabaseShowsThePasswordOnce(t *testing.T) {
	h, f, c, tok := setup(t, okAnswer("database created\n  password: S3cretPw"))
	w := followed(h, do(h, "POST", "/databases/add", url.Values{"csrf": {tok}, "site": {"blog"}, "name": {"blog_shop"}}, cookie(c)), c)
	if len(f.reqs) != 1 || f.reqs[0].Op != "db.add" || f.reqs[0].Site != "blog" || f.reqs[0].Name != "blog_shop" {
		t.Fatalf("requests %+v", f.reqs)
	}
	if !strings.Contains(w.Body.String(), "password: S3cretPw") || w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("the password must be shown, uncached:\n%s", w.Body.String())
	}
	// a later look at the lists must not show it again
	h2, _, c2, _ := setup(t, byOp(map[string]string{"db.list": `[{"name":"blog_shop","site":"blog"}]`, "site.list": blogList}))
	if strings.Contains(do(h2, "GET", "/sites/blog/databases", nil, cookie(c2)).Body.String(), "S3cretPw") {
		t.Error("the list page must never show a password")
	}
}

func TestAddingAPostgresDatabasePassesTheSelectedEngine(t *testing.T) {
	h, f, c, tok := setup(t, okAnswer("database created"))
	_ = followed(h, do(h, "POST", "/databases/add", url.Values{"csrf": {tok}, "site": {"blog"}, "name": {"shop"}, "engine": {"postgresql"}}, cookie(c)), c)
	if len(f.reqs) != 1 || f.reqs[0].Op != "db.add" || f.reqs[0].Engine != "postgresql" {
		t.Fatalf("request did not retain the selected engine: %+v", f.reqs)
	}
}

func TestPasswordChangeAndDeletionNeedTheNameTyped(t *testing.T) {
	for action, op := range map[string]string{"passwd": "db.passwd", "remove": "db.remove"} {
		h, f, c, tok := setup(t, okAnswer("done"))
		path := "/databases/" + action
		page := do(h, "GET", path+"?name=blog", nil, cookie(c)).Body.String()
		if !strings.Contains(page, "<b>blog</b>") || !strings.Contains(page, `name="confirm"`) || strings.Contains(page, `name="purge"`) {
			t.Errorf("%s: confirmation page:\n%s", action, page)
		}
		if w := do(h, "GET", path+"?name=-x", nil, cookie(c)); w.Code != 400 {
			t.Errorf("%s: invalid name: %d", action, w.Code)
		}
		if w := do(h, "POST", path, url.Values{"csrf": {tok}, "name": {"blog"}, "confirm": {"blo"}}, cookie(c)); w.Code != 400 || len(f.reqs) != 0 {
			t.Errorf("%s: wrong confirmation: %d, %d calls", action, w.Code, len(f.reqs))
		}
		if w := do(h, "POST", path, url.Values{"csrf": {tok}, "name": {"blog"}, "confirm": {"blog"}}, cookie(c)); w.Code != 303 || len(f.reqs) != 1 || f.reqs[0].Op != op || f.reqs[0].Name != "blog" {
			t.Errorf("%s: %d %+v", action, w.Code, f.reqs)
		}
	}
}

func TestDatabaseChangesNeedASessionAndAToken(t *testing.T) {
	h, f, c, _ := setup(t, okAnswer(""))
	form := url.Values{"name": {"blog"}, "confirm": {"blog"}}
	if w := do(h, "POST", "/databases/remove", form, cookie(c)); w.Code != 403 {
		t.Errorf("no token: %d", w.Code)
	}
	form.Set("csrf", "x")
	if w := do(h, "POST", "/databases/remove", form); w.Code != 303 {
		t.Errorf("no session: %d", w.Code)
	}
	for _, p := range []string{"/sites/blog/databases", "/databases/passwd?name=blog", "/databases/remove?name=blog"} {
		if w := do(h, "GET", p, nil); w.Code != 303 {
			t.Errorf("%s without a session: %d", p, w.Code)
		}
	}
	if len(f.reqs) != 0 {
		t.Errorf("refused requests reached the helper: %+v", f.reqs)
	}
}
