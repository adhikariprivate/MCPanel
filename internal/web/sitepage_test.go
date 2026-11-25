package web

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"mpn/internal/ctl"
)

const blogList = `[{"name":"blog","kind":"php","user":"blog","hostnames":["blog.example.com","www.blog.example.com"],"detail":"php 8.4","tls":true,"root":"/home/blog/htdocs"}]`

func sitePageAnswers() func(ctl.Request) (ctl.Response, error) {
	return byOp(map[string]string{"site.list": blogList, "site.access": "login set", "site.panelkey": "key put back",
		"db.list":         `[{"name":"blog","site":"blog"}]`,
		"services.status": `[{"key":"database","installed":true},{"key":"postgresql-service","installed":true}]`,
		"backup.list":     `[{"site":"blog","backup":"20261104-031500","state":"ok","files":true,"databases":["blog"],"size_mb":1.5}]`})
}

func passwdWith(t *testing.T, shell string) string {
	p := filepath.Join(t.TempDir(), "passwd")
	_ = os.WriteFile(p, []byte("root:x:0:0:root:/root:/bin/bash\nblog:x:1001:1001:mpn site blog:/home/blog:"+shell+"\n"), 0o644)
	return p
}

func TestTheSitePageUsesItsOwnSidebar(t *testing.T) {
	h, f, c, _ := setupWith(t, sitePageAnswers(), nil)
	body := do(h, "GET", "/sites/blog", nil, cookie(c)).Body.String()
	for _, want := range []string{"blog.example.com", "www.blog.example.com", "php 8.4", "/home/blog/htdocs", `href="/sites/blog/files"`,
		`href="/sites/blog/databases"`, `href="/sites/blog/backups"`, `href="/sites/blog/advanced"`, `aria-current="page"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	if !strings.Contains(body, `href="/" class="nav-link site-sidebar-back"`) || !strings.Contains(body, `class="site-sidebar-name"`) {
		t.Error("site sidebar needs a return link and site identity")
	}
	if strings.Contains(body, `class="site-tabs"`) {
		t.Error("site page still renders horizontal tabs")
	}
	if strings.Contains(body, `href="/sites/blog/activity"`) || strings.Contains(body, `href="/sites/blog/vhost"`) {
		t.Error("Activity and vHost are available outside the per-site tab bar")
	}
	for _, p := range []string{"/sites/nope", "/sites/-x", "/sites/UPPER"} {
		if w := do(h, "GET", p, nil, cookie(c)); w.Code != 404 {
			t.Errorf("%s: %d", p, w.Code)
		}
	}
	if w := do(h, "GET", "/sites/blog", nil); w.Code != 303 {
		t.Errorf("without a session: %d", w.Code)
	}
	for _, r := range f.reqs {
		if strings.ContainsAny(r.Site, " /") {
			t.Errorf("a bad name reached the helper: %+v", r)
		}
	}
}

func TestSiteSidebarFollowsGlobalEmailStateAndAdvancedActions(t *testing.T) {
	h, _, c, _ := setupWith(t, sitePageAnswers(), nil)
	overview := do(h, "GET", "/sites/blog", nil, cookie(c)).Body.String()
	if strings.Contains(overview, `href="/sites/blog/email"`) || strings.Contains(overview, `action="/sites/blog/webmail"`) {
		t.Fatal("email navigation and webmail controls are hidden when global email is off")
	}
	if strings.Contains(overview, `action="/sites/blog/domain"`) || strings.Contains(overview, `action="/sites/blog/speed"`) || strings.Contains(overview, `href="/sites/remove?site=blog"`) {
		t.Fatal("domain, acceleration, and deletion controls are outside Overview")
	}
	advanced := do(h, "GET", "/sites/blog/advanced", nil, cookie(c)).Body.String()
	if !strings.Contains(advanced, `action="/sites/blog/speed"`) || !strings.Contains(advanced, `href="/sites/remove?site=blog"`) {
		t.Fatal("Advanced contains acceleration and site deletion")
	}
}

func TestTheDatabaseAndBackupTabsAskForThisSiteOnly(t *testing.T) {
	h, f, c, _ := setupWith(t, sitePageAnswers(), nil)
	db := do(h, "GET", "/sites/blog/databases", nil, cookie(c)).Body.String()
	bk := do(h, "GET", "/sites/blog/backups", nil, cookie(c)).Body.String()
	if !strings.Contains(db, "/databases/passwd?name=blog") || !strings.Contains(bk, "backup=20261104-031500") || !strings.Contains(bk, `name="site" value="blog"`) {
		t.Errorf("tabs:\n%s\n%s", db, bk)
	}
	var asked []string
	for _, r := range f.reqs {
		if r.Op == "db.list" || r.Op == "backup.list" {
			asked = append(asked, r.Op+":"+r.Site)
		}
	}
	if !reflect.DeepEqual(asked, []string{"db.list:blog", "backup.list:blog"}) {
		t.Errorf("requests %v", asked)
	}
}

func TestTheSSHTabShowsTheStateAndTheRightButtons(t *testing.T) {
	for name, c := range map[string]struct {
		shell string
		check func(string) error
		want  []string
		never []string
	}{
		"login on, key works":   {"/bin/bash", func(string) error { return nil }, []string{"Đang bật", "Đăng nhập được", "/sites/blog/ssh/reset", "/sites/blog/ssh/disable"}, []string{"/ssh/panelkey", "/ssh/enable"}},
		"login on, key broken":  {"/bin/bash", func(string) error { return errors.New("denied") }, []string{"Đang bật", "Không đăng nhập được", "/sites/blog/ssh/panelkey"}, []string{"/ssh/enable"}},
		"login on, not checked": {"/bin/bash", nil, []string{"Đang bật", "Chưa kiểm tra"}, []string{"/ssh/panelkey"}},
		"login off":             {"/usr/sbin/nologin", func(string) error { return nil }, []string{"Đang tắt", "/sites/blog/ssh/enable"}, []string{"/ssh/reset", "/ssh/disable", "Đăng nhập được"}},
	} {
		c := c
		h, _, ck, _ := setupWith(t, sitePageAnswers(), func(s *Server) { s.PasswdFile = passwdWith(t, c.shell); s.LoginCheck = c.check })
		body := do(h, "GET", "/sites/blog/sftp", nil, cookie(ck)).Body.String()
		for _, w := range c.want {
			if !strings.Contains(body, w) {
				t.Errorf("%s: missing %q", name, w)
			}
		}
		for _, w := range c.never {
			if strings.Contains(body, w) {
				t.Errorf("%s: should not show %q", name, w)
			}
		}
	}
}

func TestResettingThePasswordNeedsTheNameTypedAndShowsTheNewOneOnce(t *testing.T) {
	h, f, c, tok := setupWith(t, sitePageAnswers(), func(s *Server) { s.PasswdFile = passwdWith(t, "/bin/bash") })
	page := do(h, "GET", "/sites/blog/ssh/reset", nil, cookie(c)).Body.String()
	if !strings.Contains(page, "<b>blog</b>") || !strings.Contains(page, `action="/sites/blog/ssh/reset"`) {
		t.Errorf("confirmation page:\n%s", page)
	}
	before := len(f.reqs)
	if w := do(h, "POST", "/sites/blog/ssh/reset", url.Values{"csrf": {tok}, "confirm": {"blo"}}, cookie(c)); w.Code != 400 {
		t.Errorf("wrong confirmation: %d", w.Code)
	}
	for _, r := range f.reqs[before:] {
		if r.Op == "site.access" {
			t.Error("a wrong confirmation reached the helper")
		}
	}
	w := do(h, "POST", "/sites/blog/ssh/reset", url.Values{"csrf": {tok}, "confirm": {"blog"}}, cookie(c))
	var acc ctl.Request
	for _, r := range f.reqs {
		if r.Op == "site.access" {
			acc = r
		}
	}
	if w.Code != 303 || !acc.Enable || acc.Site != "blog" || !regexp.MustCompile(`^[A-Za-z0-9]{24}$`).MatchString(acc.Password) {
		t.Fatalf("%d %+v", w.Code, acc)
	}
	res := do(h, "GET", "/result", nil, cookie(c)).Body.String()
	if strings.Count(res, acc.Password) != 1 || !strings.Contains(res, "tài khoản: blog") {
		t.Errorf("the account and the new password are shown once:\n%s", res)
	}
	if strings.Contains(do(h, "GET", "/sites/blog", nil, cookie(c)).Body.String(), acc.Password) {
		t.Error("the page must never show a password")
	}
}

func TestEnablingDisablingAndRestoringGoToTheHelper(t *testing.T) {
	h, f, c, tok := setupWith(t, sitePageAnswers(), nil)
	do(h, "POST", "/sites/blog/ssh/enable", url.Values{"csrf": {tok}}, cookie(c))
	do(h, "POST", "/sites/blog/ssh/disable", url.Values{"csrf": {tok}, "confirm": {"blog"}}, cookie(c))
	do(h, "POST", "/sites/blog/ssh/panelkey", url.Values{"csrf": {tok}}, cookie(c))
	var got []string
	for _, r := range f.reqs {
		switch r.Op {
		case "site.access":
			if r.Enable {
				got = append(got, "enable")
			} else {
				got = append(got, "disable")
			}
		case "site.panelkey":
			got = append(got, "panelkey")
		}
	}
	if !reflect.DeepEqual(got, []string{"enable", "disable", "panelkey"}) {
		t.Errorf("requests %v", got)
	}
}

func TestSiteChangesNeedASessionATokenAndAKnownSite(t *testing.T) {
	h, f, c, tok := setupWith(t, sitePageAnswers(), nil)
	for _, p := range []string{"/sites/blog/ssh/enable", "/sites/blog/ssh/reset", "/sites/blog/ssh/disable", "/sites/blog/ssh/panelkey"} {
		if w := do(h, "POST", p, url.Values{"confirm": {"blog"}}, cookie(c)); w.Code != 403 {
			t.Errorf("%s without a token: %d", p, w.Code)
		}
		if w := do(h, "POST", p, url.Values{"csrf": {tok}, "confirm": {"blog"}}); w.Code != 303 {
			t.Errorf("%s without a session: %d", p, w.Code)
		}
	}
	if w := do(h, "POST", "/sites/nope/ssh/enable", url.Values{"csrf": {tok}}, cookie(c)); w.Code != 404 {
		t.Errorf("unknown site: %d", w.Code)
	}
	for _, r := range f.reqs {
		if r.Op == "site.access" || r.Op == "site.panelkey" {
			t.Errorf("a refused request reached the helper: %+v", r)
		}
	}
}
