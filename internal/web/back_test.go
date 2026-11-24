package web

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"mpn/internal/ctl"
)

func TestOnlyThePanelsOwnPagesAreAcceptedAsWhereToGoBack(t *testing.T) {
	for _, ok := range []string{"/", "/sites/new", "/firewall", "/security", "/sites/blog", "/sites/blog/sftp", "/sites/blog/domain", "/sites/blog/advanced", "/sites/blog/databases",
		"/sites/blog/backups", "/sites/blog/files", "/sites/blog/terminal", "/sites/my-blog-2/databases"} {
		if got := safeBack(ok, "/default"); got != ok {
			t.Errorf("%q should be accepted, got %q", ok, got)
		}
	}
	// The standalone lists of databases and backups are gone: each site has them as tabs.
	for _, bad := range []string{"", "/databases", "/backups", "//evil.example", "///evil.example", "https://evil.example", "http://localhost/databases", "javascript:alert(1)",
		"/\\evil.example", "/databases\r\nSet-Cookie: x=1", "/databases\n", "/sites/../databases", "/sites/blog/../../x", "/sites/blog/other", "/sites/Blog/databases",
		"/sites/-x/databases", "/sites/blog/databases?x=1", "/sites/blog/databases#x", "/%2e%2e/x", "/sites//databases", "/sites/blog/databases/", "/sites",
		"/login", "/logout", "/result", "/databases/remove", "data:text/html,x", " /databases", "/databases ", "/sites/" + strings.Repeat("a", 80) + "/ssh"} {
		if got := safeBack(bad, "/default"); got != "/default" {
			t.Errorf("%q must not be accepted, got %q", bad, got)
		}
	}
}

var backLink = regexp.MustCompile(`<a class="btn-secondary" href="([^"]*)">Quay lại</a>`)

func backOf(t *testing.T, body string) string {
	t.Helper()
	m := backLink.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no back button in:\n%s", body)
	}
	return m[1]
}

// answers: every operation works, and the one site exists.
func backAnswers(r ctl.Request) (ctl.Response, error) {
	if r.Op == "site.list" {
		return ctl.Response{OK: true, Output: blogList}, nil
	}
	return ctl.Response{OK: true, Output: "done"}, nil
}

func TestAResultGoesBackToThePageItWasStartedFrom(t *testing.T) {
	h, _, c, tok := setupWith(t, backAnswers, nil)
	post := func(path string, v url.Values) string {
		v.Set("csrf", tok)
		return followed(h, do(h, "POST", path, v, cookie(c)), c).Body.String()
	}
	for _, tc := range []struct {
		name, path string
		form       url.Values
		want       string
	}{
		{"database made without a back address goes to the databases tab of the site", "/databases/add", url.Values{"site": {"blog"}}, "/sites/blog/databases"},
		{"database made on the page of the site", "/databases/add", url.Values{"site": {"blog"}, "name": {"shop"}, "back": {"/sites/blog/databases"}}, "/sites/blog/databases"},
		{"a made-up back address is ignored", "/databases/add", url.Values{"site": {"blog"}, "back": {"https://evil.example/"}}, "/sites/blog/databases"},
		{"a protocol-relative back address is ignored", "/databases/add", url.Values{"site": {"blog"}, "back": {"//evil.example"}}, "/sites/blog/databases"},
		{"database removed from the page of the site", "/databases/remove", url.Values{"name": {"blog_shop"}, "confirm": {"blog_shop"}, "back": {"/sites/blog/databases"}}, "/sites/blog/databases"},
		{"database removed without a back address goes to the home page", "/databases/remove", url.Values{"name": {"blog_shop"}, "confirm": {"blog_shop"}}, "/"},
		{"password changed from the page of the site", "/databases/passwd", url.Values{"name": {"blog"}, "confirm": {"blog"}, "back": {"/sites/blog/databases"}}, "/sites/blog/databases"},
		{"backup made without a back address goes to the home page", "/backups/create", url.Values{"site": {"blog"}}, "/"},
		{"backup made on the page of the site", "/backups/create", url.Values{"site": {"blog"}, "back": {"/sites/blog/backups"}}, "/sites/blog/backups"},
		{"backups pruned", "/backups/prune", url.Values{"site": {"blog"}, "keep": {"7"}, "confirm": {"blog"}, "back": {"/sites/blog/backups"}}, "/sites/blog/backups"},
		{"backup restored", "/backups/restore", url.Values{"site": {"blog"}, "backup": {"20261104-031500"}, "files": {"on"}, "confirm": {"blog"}, "back": {"/sites/blog/backups"}}, "/sites/blog/backups"},
		{"site applied from the list", "/sites/apply", url.Values{"site": {"blog"}}, "/"},
		{"site applied from its own page", "/sites/apply", url.Values{"site": {"blog"}, "back": {"/sites/blog"}}, "/sites/blog"},
		{"login turned on", "/sites/blog/ssh/enable", url.Values{}, "/sites/blog/sftp"},
		{"login reset", "/sites/blog/ssh/reset", url.Values{"confirm": {"blog"}}, "/sites/blog/sftp"},
		{"login turned off", "/sites/blog/ssh/disable", url.Values{"confirm": {"blog"}}, "/sites/blog/sftp"},
		{"panel key put back", "/sites/blog/ssh/panelkey", url.Values{}, "/sites/blog/sftp"},
	} {
		if got := backOf(t, post(tc.path, tc.form)); got != tc.want {
			t.Errorf("%s: the button goes to %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestAFailureGoesBackToTheSamePlaceAsASuccess(t *testing.T) {
	h, _, c, tok := setupWith(t, func(r ctl.Request) (ctl.Response, error) {
		if r.Op == "site.list" {
			return ctl.Response{OK: true, Output: blogList}, nil
		}
		return ctl.Response{Error: "it did not work"}, nil
	}, nil)
	w := followed(h, do(h, "POST", "/databases/add", url.Values{"csrf": {tok}, "site": {"blog"}, "name": {"shop"}, "back": {"/sites/blog/databases"}}, cookie(c)), c)
	body := w.Body.String()
	if !strings.Contains(body, "it did not work") || backOf(t, body) != "/sites/blog/databases" {
		t.Errorf("a helper failure:\n%s", body)
	}
	// a name refused before the helper is asked
	w = do(h, "POST", "/databases/add", url.Values{"csrf": {tok}, "site": {"blog"}, "name": {"bad name!"}, "back": {"/sites/blog/databases"}}, cookie(c))
	if w.Code != 400 || backOf(t, w.Body.String()) != "/sites/blog/databases" {
		t.Errorf("a refused name: %d\n%s", w.Code, w.Body.String())
	}
	// a wrong confirmation
	w = do(h, "POST", "/databases/remove", url.Values{"csrf": {tok}, "name": {"blog_shop"}, "confirm": {"wrong"}, "back": {"/sites/blog/databases"}}, cookie(c))
	if w.Code != 400 || backOf(t, w.Body.String()) != "/sites/blog/databases" {
		t.Errorf("a wrong confirmation: %d\n%s", w.Code, w.Body.String())
	}
	w = do(h, "POST", "/backups/prune", url.Values{"csrf": {tok}, "site": {"blog"}, "keep": {"0"}, "confirm": {"blog"}, "back": {"/sites/blog/backups"}}, cookie(c))
	if w.Code != 400 || backOf(t, w.Body.String()) != "/sites/blog/backups" {
		t.Errorf("a refused prune: %d\n%s", w.Code, w.Body.String())
	}
}

func TestANewSiteLeadsToItsPageAndAFailedOneBackToTheForm(t *testing.T) {
	h, _, c, tok := setupWith(t, backAnswers, nil)
	form := url.Values{"csrf": {tok}, "hosts": {"shop.example.com"}, "kind": {"static"}}
	if got := backOf(t, followed(h, do(h, "POST", "/sites/add", form, cookie(c)), c).Body.String()); got != "/sites/shop-example-com" {
		t.Errorf("a site that was made: %q", got)
	}
	w := do(h, "POST", "/sites/add", url.Values{"csrf": {tok}, "hosts": {""}}, cookie(c))
	if backOf(t, w.Body.String()) != "/sites/new" {
		t.Errorf("a form that was refused must lead back to the form:\n%s", w.Body.String())
	}
}

func TestTheConfirmationPagesCarryTheBackAddressThrough(t *testing.T) {
	h, _, c, _ := setupWith(t, backAnswers, nil)
	page := do(h, "GET", "/databases/remove?name=blog_shop&back=/sites/blog/databases", nil, cookie(c)).Body.String()
	if !strings.Contains(page, `name="back" value="/sites/blog/databases"`) || !strings.Contains(page, `href="/sites/blog/databases">Hủy`) {
		t.Errorf("the confirmation page must send the person back and carry the address on:\n%s", page)
	}
	page = do(h, "GET", "/databases/remove?name=blog_shop&back=https://evil.example", nil, cookie(c)).Body.String()
	if strings.Contains(page, "evil.example") || !strings.Contains(page, `name="back" value="/"`) {
		t.Errorf("a made-up address must not be carried on:\n%s", page)
	}
	h, _, c, _ = setupWith(t, sitePageAnswers(), nil) // this one needs a list of backups to show
	page = do(h, "GET", "/backups/restore?site=blog&backup=20261104-031500&back=/sites/blog/backups", nil, cookie(c)).Body.String()
	if !strings.Contains(page, `name="back" value="/sites/blog/backups"`) || !strings.Contains(page, `href="/sites/blog/backups">Hủy`) {
		t.Errorf("restore confirmation:\n%s", page)
	}
}

func TestThePagesOfOneSiteSendTheirOwnAddressWithEachAction(t *testing.T) {
	h, _, c, _ := setupWith(t, sitePageAnswers(), nil)
	db := do(h, "GET", "/sites/blog/databases", nil, cookie(c)).Body.String()
	for _, want := range []string{`name="back" value="/sites/blog/databases"`, `/databases/passwd?name=blog&amp;back=/sites/blog/databases`, `/databases/remove?name=blog&amp;back=/sites/blog/databases`} {
		if !strings.Contains(db, want) {
			t.Errorf("databases tab lacks %q", want)
		}
	}
	bk := do(h, "GET", "/sites/blog/backups", nil, cookie(c)).Body.String()
	for _, want := range []string{`name="back" value="/sites/blog/backups"`, `&amp;back=/sites/blog/backups`} {
		if !strings.Contains(bk, want) {
			t.Errorf("backups tab lacks %q", want)
		}
	}
	// the standalone lists are gone, so there is no page for them to answer
	for _, p := range []string{"/databases", "/backups"} {
		if w := do(h, "GET", p, nil, cookie(c)); w.Code != 404 {
			t.Errorf("%s must not exist any more: %d", p, w.Code)
		}
	}
}
