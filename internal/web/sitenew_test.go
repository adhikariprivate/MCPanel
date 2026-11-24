package web

import (
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"mpn/internal/ctl"
)

func TestSiteUserFromTheDomain(t *testing.T) {
	long := strings.Repeat("a", 40)
	for in, want := range map[string]string{
		"acme.io":              "acme",
		"Acme.IO":              "acme",
		"www.acme.io":          "acme",
		"blog.example.com":     "blog-example",
		"example.co.uk":        "example",
		"www.example.com.vn":   "example",
		"shop.example.co.uk":   "shop-example",
		"x--y.com":             "x-y",
		"123abc.com":           "u123abc",
		"mail.com":             "mail-web",
		"admin.org":            "admin-web",
		"systemd-foo.net":      "systemd-foo-web",
		"localhost":            "localhost",
		"":                     "site",
		"...":                  "site",
		long + ".com":          strings.Repeat("a", 32),
		"-weird_name!.example": "weird-name",
	} {
		if got := siteUserFrom(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
	// Whatever the input, the result must be a valid account name that is not reserved.
	valid := regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	for _, in := range []string{"a.b", "1.2.3.4", "ÿ.com", "mail.com", strings.Repeat("x", 80) + ".org", "root.io", "www.", "--.--"} {
		if u := siteUserFrom(in); !valid.MatchString(u) {
			t.Errorf("%q gave an invalid account name %q", in, u)
		}
	}
}

func TestSiteNameFromAHostName(t *testing.T) {
	for in, want := range map[string]string{
		"blog.example.com":               "blog-example-com",
		"Blog.Example.COM":               "blog-example-com",
		"-x.example.com":                 "x-example-com",
		"a..b---c.com":                   "a-b-c-com",
		"":                               "",
		"...":                            "",
		strings.Repeat("a", 40) + ".com": strings.Repeat("a", 31),
	} {
		if got := siteNameFrom(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func setupWith(t *testing.T, answer func(ctl.Request) (ctl.Response, error), tweak func(*Server)) (http.Handler, *fakeHelper, *http.Cookie, string) {
	t.Helper()
	f := &fakeHelper{answer: answer}
	s := newServer()
	s.Call = f.call
	s.UserExists = func(string) bool { return false }
	if tweak != nil {
		tweak(s)
	}
	h := s.Handler()
	c := cookieNamed(login(h, goodPW), "mpn")
	m := csrfRe.FindStringSubmatch(do(h, "GET", "/", nil, cookie(c)).Body.String())
	if c == nil || m == nil {
		t.Fatal("could not sign in")
	}
	f.reqs = nil
	return h, f, c, m[1]
}

func createAnswers() func(ctl.Request) (ctl.Response, error) {
	return byOp(map[string]string{"site.list": "[]", "site.add": "added", "site.apply": "applied", "site.access": "login enabled"})
}

func TestTheWebsitesPageHasAButtonAndTheChoicePageHasTheKinds(t *testing.T) {
	h, _, c, _ := setup(t, byOp(map[string]string{"site.list": "[]"}))
	body := do(h, "GET", "/", nil, cookie(c)).Body.String()
	if !strings.Contains(body, `href="/sites/new"`) || strings.Contains(body, `action="/sites/add"`) {
		t.Errorf("the list page must link to the new-site page and not hold the form:\n%s", body)
	}
	choice := do(h, "GET", "/sites/new", nil, cookie(c)).Body.String()
	for _, k := range []string{"php", "binary", "mise", "static", "proxy"} {
		if !strings.Contains(choice, `href="/sites/new?kind=`+k+`"`) {
			t.Errorf("no card for %s", k)
		}
	}
	if strings.Contains(choice, `name="hosts"`) {
		t.Error("the choice page must not show the form yet")
	}
	if w := do(h, "GET", "/sites/new?kind=ruby", nil, cookie(c)); w.Code != 400 {
		t.Errorf("unknown kind: %d", w.Code)
	}
	if w := do(h, "GET", "/sites/new", nil); w.Code != 303 {
		t.Errorf("without a session: %d", w.Code)
	}
}

func TestTheFormAsksForLittleAndAlwaysGivesLoginAccess(t *testing.T) {
	h, _, c, _ := setup(t, okAnswer(""))
	extra := map[string][]string{"php": {`name="php"`}, "binary": {`name="entry"`}, "mise": {}, "proxy": {`name="upstream"`}, "static": {}}
	never := map[string][]string{"php": {`name="entry"`, `name="upstream"`}, "binary": {`name="php"`, `name="upstream"`}, "mise": {`name="php"`, `name="entry"`},
		"proxy": {`name="php"`, `name="entry"`}, "static": {`name="php"`, `name="entry"`, `name="upstream"`}}
	for kind := range extra {
		body := do(h, "GET", "/sites/new?kind="+kind, nil, cookie(c)).Body.String()
		for _, f := range append(extra[kind], `name="hosts"`, `data-hosts`, `name="user"`, `data-user`, `data-password`, `name="password"`,
			`data-gen-password="f-pass"`, `name="csrf"`, `name="kind" value="`+kind+`"`) {
			if !strings.Contains(body, f) {
				t.Errorf("%s: missing %s", kind, f)
			}
		}
		for _, f := range append(never[kind], `name="access"`, `name="name"`) {
			if strings.Contains(body, f) {
				t.Errorf("%s: must not show %s", kind, f)
			}
		}
		if !strings.Contains(body, `data-reserved="`) || !strings.Contains(body, "root") {
			t.Errorf("%s: the page must hand the reserved names to the script", kind)
		}
	}
}

func TestCreatingASiteUsesTheDerivedAccountAndShowsTheLoginOnce(t *testing.T) {
	h, f, c, tok := setupWith(t, createAnswers(), nil)
	form := url.Values{"csrf": {tok}, "kind": {"php"}, "hosts": {"Acme.IO"}, "php": {"8.4"}}
	w := do(h, "POST", "/sites/add", form, cookie(c))
	if w.Code != 303 || w.Header().Get("Location") != "/result" || strings.Contains(w.Body.String(), "acme") {
		t.Fatalf("the post must only redirect: %d", w.Code)
	}
	if got := f.ops(); !reflect.DeepEqual(got, []string{"site.list", "site.add", "site.apply", "site.access"}) {
		t.Fatalf("ops %v", got)
	}
	add, acc := f.reqs[1], f.reqs[3]
	if add.Site != "acme-io" || add.User != "acme" || !reflect.DeepEqual(add.Hosts, []string{"acme.io"}) {
		t.Errorf("name, account and hosts must come from the domain: %+v", add)
	}
	if !acc.Enable || acc.Site != "acme-io" || !regexp.MustCompile(`^[A-Za-z0-9]{24}$`).MatchString(acc.Password) {
		t.Errorf("login access with a random 24-character password is not optional: %+v", acc)
	}
	page := do(h, "GET", "/result", nil, cookie(c)).Body.String()
	if !strings.Contains(page, "tài khoản: acme") || strings.Count(page, acc.Password) != 1 || !strings.Contains(page, "localhost") {
		t.Errorf("the account and the password must be shown once, with the host:\n%s", page)
	}
	if again := do(h, "GET", "/result", nil, cookie(c)); strings.Contains(again.Body.String(), acc.Password) {
		t.Error("the password must be gone after the first look")
	}
}

func TestTypedAccountAndPasswordAreUsedAndStillShownOnce(t *testing.T) {
	h, f, c, tok := setupWith(t, createAnswers(), nil)
	form := url.Values{"csrf": {tok}, "kind": {"static"}, "hosts": {"shop.example.com"}, "user": {"shopper"}, "password": {"Typed-Pass-12345"}}
	do(h, "POST", "/sites/add", form, cookie(c))
	if f.reqs[1].User != "shopper" || f.reqs[3].Password != "Typed-Pass-12345" {
		t.Errorf("requests %+v", f.reqs)
	}
	if page := do(h, "GET", "/result", nil, cookie(c)).Body.String(); strings.Count(page, "Typed-Pass-12345") != 1 {
		t.Errorf("the credentials are shown once on the result page:\n%s", page)
	}
}

func TestRefusedBeforeAnythingIsCreated(t *testing.T) {
	existing := `[{"name":"old","kind":"php","user":"acme","hostnames":["old.example.com"],"detail":"","tls":false},
{"name":"shop-example-com","kind":"php","user":"someoneelse","hostnames":["x.example.com"],"detail":"","tls":false}]`
	answers := byOp(map[string]string{"site.list": existing, "site.add": "added", "site.apply": "applied", "site.access": "ok"})
	h, f, c, tok := setupWith(t, answers, func(s *Server) { s.UserExists = func(n string) bool { return n == "ubuntuuser" } })
	cases := map[string]url.Values{
		"no hosts":            {"hosts": {" , "}},
		"two domains":         {"hosts": {"a.example.com b.example.com"}},
		"unusable hosts":      {"hosts": {"..."}},
		"reserved account":    {"hosts": {"new.example.com"}, "user": {"root"}},
		"existing account":    {"hosts": {"new.example.com"}, "user": {"ubuntuuser"}},
		"account of a site":   {"hosts": {"acme.io"}},
		"site name in use":    {"hosts": {"shop.example.com"}},
		"short password":      {"hosts": {"new.example.com"}, "password": {"short"}},
		"password with space": {"hosts": {"new.example.com"}, "password": {"has a space in it"}},
	}
	for name, v := range cases {
		v.Set("csrf", tok)
		v.Set("kind", "static")
		before := len(f.reqs)
		if w := do(h, "POST", "/sites/add", v, cookie(c)); w.Code != 400 {
			t.Errorf("%s: %d", name, w.Code)
		}
		for _, r := range f.reqs[before:] {
			if r.Op != "site.list" {
				t.Errorf("%s: reached the helper with %s", name, r.Op)
			}
		}
	}
}

func TestNoLoginIsSetUpWhenCreatingFails(t *testing.T) {
	h, f, c, tok := setupWith(t, func(r ctl.Request) (ctl.Response, error) {
		switch r.Op {
		case "site.list":
			return ctl.Response{OK: true, Output: "[]"}, nil
		case "site.apply":
			return ctl.Response{Error: "nginx failed"}, nil
		}
		return ctl.Response{OK: true}, nil
	}, nil)
	form := url.Values{"csrf": {tok}, "kind": {"static"}, "hosts": {"shop.example.com"}}
	page := followed(h, do(h, "POST", "/sites/add", form, cookie(c)), c).Body.String()
	if got := f.ops(); !reflect.DeepEqual(got, []string{"site.list", "site.add", "site.apply"}) || !strings.Contains(page, "nginx failed") {
		t.Errorf("ops %v\n%s", got, page)
	}
	if strings.Contains(page, "mật khẩu:") {
		t.Error("no password may be shown when the site was not made")
	}
}
