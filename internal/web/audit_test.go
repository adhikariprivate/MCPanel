package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"mpn/internal/ctl"
	"mpn/internal/webauth"
)

func notes(f *fakeHelper) []ctl.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n []ctl.Request
	for _, r := range f.reqs {
		if r.Op == "audit.note" {
			n = append(n, r)
		}
	}
	return n
}

func TestDelegatedActivityOnlyQueriesAssignedSites(t *testing.T) {
	const blogID = "00000000000000000000000000000001"
	const shopID = "00000000000000000000000000000002"
	access := webauth.NewDelegatedAccess()
	access.Users["editor"] = webauth.NewDelegatedUser("stored-hash", map[string]map[string]string{
		blogID: {"site.view": "allow"},
	})
	path := filepath.Join(t.TempDir(), "access.json")
	if err := webauth.SaveDelegatedAccess(path, access); err != nil {
		t.Fatal(err)
	}
	f := &fakeHelper{answer: func(r ctl.Request) (ctl.Response, error) {
		switch r.Op {
		case "site.list":
			return ctl.Response{OK: true, Output: `[{"name":"blog","access_id":"` + blogID + `"},{"name":"shop","access_id":"` + shopID + `"}]`}, nil
		case "audit.list":
			return ctl.Response{OK: true, Output: `{"entries":[],"total":0}`}, nil
		default:
			return ctl.Response{OK: true, Output: "[]"}, nil
		}
	}}
	s := newServer()
	s.Call, s.AccessFile = f.call, path
	token, _ := s.Sessions.Create("editor")
	h := s.Handler()
	ck := cookie(&http.Cookie{Name: "mpn", Value: token})
	list := do(h, "GET", "/", nil, ck)
	if list.Code != 200 || strings.Contains(list.Body.String(), `href="/sites/new"`) || !strings.Contains(list.Body.String(), "blog") {
		t.Fatalf("delegated site list must hide Add website and show assigned sites: %d\n%s", list.Code, list.Body.String())
	}
	w := do(h, "GET", "/activity", nil, ck)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `value="blog"`) || strings.Contains(w.Body.String(), `value="shop"`) {
		t.Fatalf("delegated activity filter was not limited to the assigned site: %d\n%s", w.Code, w.Body.String())
	}
	r := lastList(f, 0)
	if r == nil || r.Site != "" || len(r.AuditSites) != 1 || r.AuditSites[0] != "blog" {
		t.Fatalf("activity query did not carry the assigned site scope: %+v", r)
	}
	before := len(f.reqs)
	if w := do(h, "GET", "/activity.csv?site=shop", nil, ck); w.Code != 403 {
		t.Fatalf("unassigned site export status %d", w.Code)
	}
	if lastList(f, before) != nil {
		t.Fatal("the helper must not receive an unassigned site export")
	}
}

func TestActivitySidebarVisibilityFollowsAssignedSiteView(t *testing.T) {
	const siteID = "00000000000000000000000000000011"
	for _, tc := range []struct {
		name, capability string
		wantActivity     bool
	}{
		{"assigned site viewer", "site.view", true},
		{"assigned site without overview access", "files.read", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			access := webauth.NewDelegatedAccess()
			access.Users["editor"] = webauth.NewDelegatedUser("stored-hash", map[string]map[string]string{
				siteID: {tc.capability: "allow"},
			})
			path := filepath.Join(t.TempDir(), "access.json")
			if err := webauth.SaveDelegatedAccess(path, access); err != nil {
				t.Fatal(err)
			}
			s := newServer()
			s.AccessFile = path
			s.Call = func(r ctl.Request) (ctl.Response, error) {
				if r.Op == "site.list" {
					return ctl.Response{OK: true, Output: `[{"name":"blog","access_id":"` + siteID + `"}]`}, nil
				}
				return ctl.Response{OK: true, Output: "[]"}, nil
			}
			w := httptest.NewRecorder()
			s.show(w, http.StatusOK, webauth.Session{User: "editor"}, view{Page: "sites"})
			body := w.Body.String()
			if got := strings.Contains(body, `href="/activity"`); got != tc.wantActivity {
				t.Errorf("Activity link visible=%t, want %t", got, tc.wantActivity)
			}
			if strings.Contains(body, `href="/monitor"`) {
				t.Error("Monitor must remain owner-only")
			}
		})
	}

	owner := newServer()
	w := httptest.NewRecorder()
	owner.show(w, http.StatusOK, webauth.Session{User: "admin"}, view{Page: "sites"})
	if !strings.Contains(w.Body.String(), `href="/activity"`) || !strings.Contains(w.Body.String(), `href="/monitor"`) {
		t.Error("instance administrator should still see Activity and Monitor")
	}
}

// Signing in, a wrong password and signing out are told to the helper with the address; a wrong name is not kept as typed, because
// it is sometimes the password.
func TestSignInsGoToTheLog(t *testing.T) {
	f := &fakeHelper{answer: okAnswer("")}
	s := newServer()
	s.Call = f.call
	h := s.Handler()
	do(h, "POST", "/login", url.Values{"user": {"MyS3cretPass"}, "password": {"x"}}, addr("203.0.113.9:1"))
	c := cookieNamed(login(h, goodPW), "mpn")
	n := notes(f)
	if len(n) != 2 || n[0].Event != "login.fail" || !n[0].Failed || n[0].Actor != "203.0.113.9" || n[0].Note != "user=other" ||
		n[1].Event != "login.ok" || n[1].Failed {
		t.Fatalf("%+v", n)
	}
	m := csrfRe.FindStringSubmatch(do(h, "GET", "/", nil, cookie(c)).Body.String())
	do(h, "POST", "/logout", url.Values{"csrf": {m[1]}}, cookie(c))
	if n = notes(f); n[len(n)-1].Event != "logout" {
		t.Errorf("%+v", n)
	}
}

// Every change sent to the helper carries the address of the person.
func TestChangesCarryTheAddress(t *testing.T) {
	h, f, c, tok := setup(t, okAnswer(""))
	do(h, "POST", "/databases/add", url.Values{"csrf": {tok}, "site": {"blog"}, "name": {"blog_shop"}}, cookie(c))
	seen := false
	for _, r := range f.reqs {
		if r.Op == "db.add" {
			seen = true
			if r.Actor != "192.0.2.1" {
				t.Errorf("no address: %+v", r)
			}
		}
	}
	if !seen {
		t.Error("db.add was not sent")
	}
}

func lastList(f *fakeHelper, from int) *ctl.Request {
	var asked *ctl.Request
	for i := from; i < len(f.reqs); i++ {
		if f.reqs[i].Op == "audit.list" {
			asked = &f.reqs[i]
		}
	}
	return asked
}

func TestTheActivityPageShowsTheLogAndExportsIt(t *testing.T) {
	log := `{"entries":[{"id":1,"at":1763600000,"ip":"1.1.1.1","site":"blog","op":"db.add","kind":"database","detail":"name=blog_<b>","ok":true},` +
		`{"id":2,"at":1763600100,"ip":"1.1.1.1","site":"blog","op":"db.remove","kind":"database","detail":"name=blog_x","ok":true,"danger":true}],"total":2}`
	h, f, c, _ := setup(t, func(r ctl.Request) (ctl.Response, error) {
		if r.Op == "audit.list" {
			return ctl.Response{OK: true, Output: log}, nil
		}
		if r.Op == "site.list" {
			return ctl.Response{OK: true, Output: `[{"name":"blog","kind":"php","user":"blog","hostnames":["blog.example.com"]}]`}, nil
		}
		return ctl.Response{OK: true, Output: "[]"}, nil
	})
	body := do(h, "GET", "/activity?site=blog&kind=database&ip=1.1.", nil, cookie(c)).Body.String()
	for _, w := range []string{"Tạo cơ sở dữ liệu", "Xóa cơ sở dữ liệu", "name=blog_&lt;b&gt;", "1.1.1.1", "Quan trọng"} {
		if !strings.Contains(body, w) {
			t.Errorf("page lacks %q", w)
		}
	}
	asked := lastList(f, 0)
	if asked == nil || asked.Site != "blog" || asked.AuditKind != "database" || asked.AuditIP != "1.1." || asked.Limit != auditPageSize {
		t.Fatalf("%+v", asked)
	}
	w := do(h, "GET", "/activity.csv?site=blog", nil, cookie(c))
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") || !strings.Contains(w.Body.String(), "db.remove") ||
		!strings.Contains(w.Header().Get("Content-Disposition"), "mpn-activity-blog-") {
		t.Errorf("csv: %v\n%s", w.Header(), w.Body)
	}
	// Per-site Activity is replaced by the sidebar Activity page.
	n := len(f.reqs)
	if w := do(h, "GET", "/sites/blog/activity", nil, cookie(c)); w.Code != 404 {
		t.Errorf("per-site activity tab status %d", w.Code)
	}
	if lastList(f, n) != nil {
		t.Error("the removed per-site page must not fetch activity")
	}
	// A filter that is not valid never reaches the helper.
	n = len(f.reqs)
	do(h, "GET", "/activity?ip=1.1.1.1%20-db=/x&kind=nope&from=yesterday", nil, cookie(c))
	if r := lastList(f, n); r == nil || r.AuditIP != "" || r.AuditKind != "" || r.From != "" {
		t.Errorf("bad filter passed on: %+v", r)
	}
}
