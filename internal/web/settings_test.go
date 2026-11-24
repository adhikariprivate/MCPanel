package web

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"mpn/internal/webauth"
)

// acctStore stands in for the account file: the panel reads it on every sign-in and writes it when a setting changes.
type acctStore struct {
	mu sync.Mutex
	a  webauth.Account
}

func (s *acctStore) get() (webauth.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.a, nil
}

func (s *acctStore) set(a webauth.Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.a = a
	return nil
}

func settingsHandler(t *testing.T) (http.Handler, *acctStore, *http.Cookie, string) {
	t.Helper()
	st := &acctStore{a: account()}
	h, _, c, csrf := setupWith(t, byOp(nil), func(s *Server) { s.Account, s.SaveAccount = st.get, st.set })
	return h, st, c, csrf
}

var errSaveFailed = errors.New("disk full")

var (
	secretRe  = regexp.MustCompile(`data-copy="([A-Z2-7]{32})"`)
	pendingRe = regexp.MustCompile(`name="pending" value="([^"]+)"`)
)

func post(h http.Handler, path string, csrf string, c *http.Cookie, kv ...string) *http.Response {
	form := url.Values{"csrf": {csrf}}
	for i := 0; i+1 < len(kv); i += 2 {
		form.Set(kv[i], kv[i+1])
	}
	return do(h, "POST", path, form, cookie(c)).Result()
}

// turnOn2FA runs the real setup and returns the secret the app would hold.
func turnOn2FA(t *testing.T, h http.Handler, c *http.Cookie, csrf string) string {
	t.Helper()
	page := do(h, "GET", "/settings/2fa", nil, cookie(c)).Body.String()
	m := secretRe.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no secret on the setup page:\n%s", page)
	}
	code, _ := webauth.CodeAt(m[1], time.Now())
	if r := post(h, "/settings/2fa/enable", csrf, c, "code", code); r.StatusCode != 303 {
		t.Fatalf("enable: %d", r.StatusCode)
	}
	return m[1]
}

func TestTheNameInTheHeaderOpensAMenuWithSettingsAndSignOut(t *testing.T) {
	h, _, c, _ := settingsHandler(t)
	body := do(h, "GET", "/", nil, cookie(c)).Body.String()
	panel := strings.Index(body, "data-menu-panel")
	if panel < 0 || !strings.Contains(body, "data-menu-button") || !strings.Contains(body, `aria-haspopup="menu"`) {
		t.Fatal("no menu on the name")
	}
	rest := body[panel:]
	if !strings.Contains(rest, `href="/settings"`) || !strings.Contains(rest, `action="/logout"`) || !strings.Contains(rest, "Cài đặt") || !strings.Contains(rest, "Đăng xuất") {
		t.Error("settings and sign out belong in the menu")
	}
	if strings.Index(body, `action="/logout"`) < panel {
		t.Error("the sign-out button must not stand outside the menu any more")
	}
	if w := do(h, "GET", "/settings", nil); w.Code != 303 {
		t.Errorf("settings without a session: %d", w.Code)
	}
}

func TestSettingsShowsThePasswordAndTheTwoStepCards(t *testing.T) {
	h, _, c, _ := settingsHandler(t)
	body := do(h, "GET", "/settings", nil, cookie(c)).Body.String()
	for _, want := range []string{`action="/settings/password"`, `name="current"`, `name="new"`, `name="again"`, "Đang tắt", `href="/settings/2fa"`, "mpn web 2fa-off"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestChangingThePassword(t *testing.T) {
	h, st, c, csrf := settingsHandler(t)
	other := cookieNamed(login(h, goodPW, addr("198.51.100.9:1")), "mpn") // a second browser, signed in with the old password
	const newPW = "a-brand-new-password-9"
	for _, bad := range []struct {
		name, current, new, again string
		code                      int
	}{
		{"wrong current", "not-it", newPW, newPW, 401},
		{"too short", goodPW, "short", "short", 400},
		{"mismatch", goodPW, newPW, newPW + "x", 400},
		{"same as the old one", goodPW, goodPW, goodPW, 400},
	} {
		if r := post(h, "/settings/password", csrf, c, "current", bad.current, "new", bad.new, "again", bad.again); r.StatusCode != bad.code {
			t.Errorf("%s: %d, want %d", bad.name, r.StatusCode, bad.code)
		}
	}
	if a, _ := st.get(); !webauth.Verify(goodPW, a.Hash) {
		t.Fatal("a refused change must leave the password alone")
	}
	if r := post(h, "/settings/password", "", c, "current", goodPW, "new", newPW, "again", newPW); r.StatusCode != 403 {
		t.Errorf("without the form token: %d", r.StatusCode)
	}
	if r := post(h, "/settings/password", csrf, c, "current", goodPW, "new", newPW, "again", newPW); r.StatusCode != 303 {
		t.Fatalf("change: %d", r.StatusCode)
	}
	if note := do(h, "GET", "/settings", nil, cookie(c)).Body.String(); !strings.Contains(note, "Đã đổi mật khẩu") {
		t.Error("the page says it was done")
	}
	if login(h, goodPW, addr("198.51.100.20:1")).Code != 401 || login(h, newPW, addr("198.51.100.21:1")).Code != 303 {
		t.Error("the new password works and the old one does not")
	}
	if do(h, "GET", "/settings", nil, cookie(c)).Code != 200 {
		t.Error("this browser stays signed in")
	}
	if do(h, "GET", "/settings", nil, cookie(other)).Code != 303 {
		t.Error("the other browser is signed out")
	}
}

func TestTheSecondStepOfTheSignIn(t *testing.T) {
	h, st, c, csrf := settingsHandler(t)
	// The setup page: a QR code as an inline SVG (the page allows no data: images), the secret to type, and no script.
	page := do(h, "GET", "/settings/2fa", nil, cookie(c)).Body.String()
	if !strings.Contains(page, "<svg xmlns") || !strings.Contains(page, `shape-rendering="crispEdges"`) || !strings.Contains(page, `action="/settings/2fa/enable"`) {
		t.Error("the setup page shows a code and asks for the first code of the app")
	}
	secret := secretRe.FindStringSubmatch(page)[1]
	if a, _ := st.get(); a.TOTP != "" {
		t.Error("nothing is stored until a code proves that the app has the secret")
	}
	if r := post(h, "/settings/2fa/enable", csrf, c, "code", "000000"); r.StatusCode != 401 {
		t.Errorf("a wrong code: %d", r.StatusCode)
	}
	if a, _ := st.get(); a.TOTP != "" {
		t.Error("a wrong code must not switch it on")
	}
	good, _ := webauth.CodeAt(secret, time.Now())
	if r := post(h, "/settings/2fa/enable", csrf, c, "code", good); r.StatusCode != 303 {
		t.Fatalf("the right code: %d", r.StatusCode)
	}
	if a, _ := st.get(); a.TOTP != secret || a.TOTPStep == 0 {
		t.Fatalf("stored: %+v", a)
	}

	// Signing in now takes two steps.
	w := login(h, goodPW, addr("198.51.100.30:1"))
	if w.Code != 200 || len(w.Result().Cookies()) != 0 || !strings.Contains(w.Body.String(), `action="/login/2fa"`) {
		t.Fatalf("after the password: %d, cookies %v", w.Code, w.Result().Cookies())
	}
	pending := pendingRe.FindStringSubmatch(w.Body.String())[1]
	step2 := func(token, code string) *http.Response {
		return do(h, "POST", "/login/2fa", url.Values{"pending": {token}, "code": {code}}, addr("198.51.100.30:1")).Result()
	}
	if r := step2(pending, "000000"); r.StatusCode != 401 || len(r.Cookies()) != 0 {
		t.Errorf("a wrong code: %d", r.StatusCode)
	}
	next, _ := webauth.CodeAt(secret, time.Now().Add(30*time.Second)) // the enable step is used; the next one is fresh
	r := step2(pending, next)
	if r.StatusCode != 303 || len(r.Cookies()) == 0 {
		t.Fatalf("the right code: %d", r.StatusCode)
	}
	// The same code cannot be used twice, not even in a sign-in of its own.
	again := login(h, goodPW, addr("198.51.100.31:1"))
	p2 := pendingRe.FindStringSubmatch(again.Body.String())[1]
	if r := do(h, "POST", "/login/2fa", url.Values{"pending": {p2}, "code": {next}}, addr("198.51.100.31:1")).Result(); r.StatusCode != 401 {
		t.Errorf("a used code was accepted again: %d", r.StatusCode)
	}
	// A sign-in that never passed the password cannot be finished with a code.
	if r := step2("made-up-token", next); r.StatusCode != 401 {
		t.Errorf("a token that was never given: %d", r.StatusCode)
	}
}

// The cases of the report: a code is used once even when two sign-ins send it at the same moment, and a code that cannot be marked as
// used gives no session.
func TestACodeWorksOnceEvenWhenSentTwiceAtOnce(t *testing.T) {
	h, st, c, csrf := settingsHandler(t)
	secret := turnOn2FA(t, h, c, csrf)
	var pendings []string
	for i := 0; i < 6; i++ {
		w := login(h, goodPW, addr("198.51.100.5"+string(rune('0'+i))+":1"))
		pendings = append(pendings, pendingRe.FindStringSubmatch(w.Body.String())[1])
	}
	code, _ := webauth.CodeAt(secret, time.Now().Add(30*time.Second))
	var wg sync.WaitGroup
	var mu sync.Mutex
	signedIn := 0
	for i, p := range pendings {
		wg.Add(1)
		go func(i int, p string) {
			defer wg.Done()
			r := do(h, "POST", "/login/2fa", url.Values{"pending": {p}, "code": {code}}, addr("198.51.100.5"+string(rune('0'+i))+":1")).Result()
			if r.StatusCode == 303 && len(r.Cookies()) > 0 {
				mu.Lock()
				signedIn++
				mu.Unlock()
			}
		}(i, p)
	}
	wg.Wait()
	if signedIn != 1 {
		t.Errorf("%d sign-ins with one code, want 1", signedIn)
	}
	_ = st
}

func TestNoSessionWhenTheUsedCodeCannotBeSaved(t *testing.T) {
	st := &acctStore{a: account()}
	failing := false
	save := func(a webauth.Account) error {
		if failing {
			return errSaveFailed
		}
		return st.set(a)
	}
	h, _, c, csrf := setupWith(t, byOp(nil), func(s *Server) { s.Account, s.SaveAccount = st.get, save })
	secret := turnOn2FA(t, h, c, csrf)
	w := login(h, goodPW, addr("198.51.100.60:1"))
	pending := pendingRe.FindStringSubmatch(w.Body.String())[1]
	failing = true
	code, _ := webauth.CodeAt(secret, time.Now().Add(30*time.Second))
	r := do(h, "POST", "/login/2fa", url.Values{"pending": {pending}, "code": {code}}, addr("198.51.100.60:1")).Result()
	if r.StatusCode == 303 || len(r.Cookies()) != 0 {
		t.Errorf("a session was given although the code could not be marked as used: %d %v", r.StatusCode, r.Cookies())
	}
}

func TestAFewWrongCodesEndTheSignIn(t *testing.T) {
	h, _, c, csrf := settingsHandler(t)
	secret := turnOn2FA(t, h, c, csrf)
	w := login(h, goodPW, addr("198.51.100.40:1"))
	pending := pendingRe.FindStringSubmatch(w.Body.String())[1]
	for i := 0; i < maxCodeTries; i++ {
		do(h, "POST", "/login/2fa", url.Values{"pending": {pending}, "code": {"000000"}}, addr("198.51.100.40:1"))
	}
	good, _ := webauth.CodeAt(secret, time.Now().Add(30*time.Second))
	if r := do(h, "POST", "/login/2fa", url.Values{"pending": {pending}, "code": {good}}, addr("198.51.100.40:1")).Result(); r.StatusCode == 303 {
		t.Error("after five wrong codes even the right one must not finish that sign-in")
	}
}

func TestSwitchingTheSecondStepOffNeedsThePasswordAndACode(t *testing.T) {
	h, st, c, csrf := settingsHandler(t)
	secret := turnOn2FA(t, h, c, csrf)
	code, _ := webauth.CodeAt(secret, time.Now().Add(30*time.Second))
	if r := post(h, "/settings/2fa/disable", csrf, c, "password", "wrong-password-here", "code", code); r.StatusCode != 401 {
		t.Errorf("a wrong password: %d", r.StatusCode)
	}
	if r := post(h, "/settings/2fa/disable", csrf, c, "password", goodPW, "code", "000000"); r.StatusCode != 401 {
		t.Errorf("a wrong code: %d", r.StatusCode)
	}
	if a, _ := st.get(); a.TOTP == "" {
		t.Fatal("a refused request must leave it on")
	}
	if page := do(h, "GET", "/settings", nil, cookie(c)).Body.String(); !strings.Contains(page, "Đang bật") || !strings.Contains(page, `action="/settings/2fa/disable"`) {
		t.Error("the page shows that it is on and how to turn it off")
	}
	if r := post(h, "/settings/2fa/disable", csrf, c, "password", goodPW, "code", code); r.StatusCode != 303 {
		t.Fatalf("both right: %d", r.StatusCode)
	}
	if a, _ := st.get(); a.TOTP != "" || a.TOTPStep != 0 {
		t.Errorf("it is off: %+v", a)
	}
	if w := login(h, goodPW, addr("198.51.100.50:1")); w.Code != 303 {
		t.Errorf("with the second step off the password is enough: %d", w.Code)
	}
}

func TestGuessingFromInsideASessionIsLimited(t *testing.T) {
	h, _, c, csrf := settingsHandler(t)
	var last int
	for i := 0; i < 12; i++ {
		last = post(h, "/settings/password", csrf, c, "current", "guess", "new", "another-long-password", "again", "another-long-password").StatusCode
	}
	if last != 429 {
		t.Errorf("a session must not be a way to guess the password without limit: %d", last)
	}
}

func TestTheQRCodeIsAnInlineImageThatNeedsNoScript(t *testing.T) {
	svg, err := qrSVG(webauth.TOTPURI("MCPN", "admin", "JBSWY3DPEHPK3PXP"), `a "quoted" <label>`)
	if err != nil {
		t.Fatal(err)
	}
	s := string(svg)
	if !strings.HasPrefix(s, "<svg") || !strings.Contains(s, `role="img"`) || strings.Contains(s, "<script") || strings.Contains(s, `<label>`) {
		t.Errorf("%s", s[:min(len(s), 300)])
	}
	if !strings.Contains(s, "&lt;label&gt;") {
		t.Error("the alternative text is escaped")
	}
}
