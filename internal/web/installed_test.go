package web

import (
	"net/url"
	"strings"
	"testing"

	"mpn/internal/ctl"
)

// machine reports the PHP versions installed on the server.
func machine(php []string) func(*Server) {
	installed := append([]string{}, php...)
	return func(s *Server) { s.InstalledPHP = func() []string { return installed } }
}

func TestTheFormOffersOnlyVersionsThatAreInstalled(t *testing.T) {
	h, _, c, _ := setupWith(t, createAnswers(), machine([]string{"8.3", "8.4"}))
	body := do(h, "GET", "/sites/new?kind=php", nil, cookie(c)).Body.String()
	if !strings.Contains(body, "<option>8.3</option>") || !strings.Contains(body, "<option selected>8.4</option>") || strings.Contains(body, "<option>7.4</option>") || strings.Contains(body, "<option>8.5</option>") {
		t.Errorf("php versions offered: %s", body)
	}
}

func TestTheFormSaysSoWhenNothingIsInstalled(t *testing.T) {
	h, _, c, _ := setupWith(t, createAnswers(), machine(nil))
	if body := do(h, "GET", "/sites/new?kind=php", nil, cookie(c)).Body.String(); !strings.Contains(body, "Chưa cài phiên bản PHP nào") || strings.Contains(body, `name="php"`) {
		t.Errorf("php: %s", body)
	}
	// Kinds that need no runtime are not affected.
	if body := do(h, "GET", "/sites/new?kind=static", nil, cookie(c)).Body.String(); strings.Contains(body, "Chưa cài") {
		t.Error("a static site needs no PHP or Node")
	}
}

func TestAVersionThatIsNotInstalledIsRefusedBeforeAnythingIsMade(t *testing.T) {
	for kind, form := range map[string]url.Values{
		"php": {"kind": {"php"}, "hosts": {"new.example.com"}, "php": {"7.4"}},
	} {
		h, f, c, tok := setupWith(t, createAnswers(), machine([]string{"8.4"}))
		form.Set("csrf", tok)
		w := do(h, "POST", "/sites/add", form, cookie(c))
		if w.Code != 400 || !strings.Contains(w.Body.String(), "chưa được cài") {
			t.Errorf("%s: %d\n%s", kind, w.Code, w.Body.String())
		}
		if got := f.ops(); len(got) != 0 && (len(got) != 1 || got[0] != "site.list") {
			t.Errorf("%s: the helper was asked: %v", kind, got)
		}
	}
	h, f, c, tok := setupWith(t, createAnswers(), machine([]string{"8.4"}))
	w := do(h, "POST", "/sites/add", url.Values{"csrf": {tok}, "kind": {"php"}, "hosts": {"ok.example.com"}, "php": {"8.4"}}, cookie(c))
	if w.Code != 303 || len(f.ops()) == 0 {
		t.Errorf("an installed version must be accepted: %d %v", w.Code, f.ops())
	}
}

func TestExitStatusIsReplacedByASentenceThatPointsToTheDetails(t *testing.T) {
	h, _, c, tok := setupWith(t, func(r ctl.Request) (ctl.Response, error) {
		if r.Op == "site.apply" {
			return ctl.Response{Output: "php-fpm8.4: command not found", Error: "exit status 2"}, nil
		}
		return createAnswers()(r)
	}, nil)
	w := followed(h, do(h, "POST", "/sites/add", url.Values{"csrf": {tok}, "kind": {"static"}, "hosts": {"new.example.com"}}, cookie(c)), c)
	body := w.Body.String()
	if strings.Contains(body, "exit status 2") && !strings.Contains(body, "command not found") {
		t.Error("the details are lost")
	}
	if !strings.Contains(body, "Lệnh không thực hiện được") || !strings.Contains(body, "command not found") {
		t.Errorf("a plain sentence and the details are both expected:\n%s", body)
	}
	if strings.Contains(strings.Split(body, "alert-error")[1], "exit status") {
		t.Error("the alert must not say \"exit status\"")
	}
	if plainError("some other message") != "some other message" || plainError("exit status 127") == "exit status 127" {
		t.Error("plainError")
	}
}
