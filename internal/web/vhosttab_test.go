package web

import (
	"net/url"
	"strings"
	"testing"

	"mpn/internal/ctl"
	"mpn/internal/vhost"
)

func vhostAnswers(custom bool) func(ctl.Request) (ctl.Response, error) {
	text := "server {\\n    listen 80;\\n    server_name example.com;\\n}\\n"
	info := `{"custom":` + map[bool]string{true: "true", false: "false"}[custom] + `,"text":"` + text + `","tls":false}`
	return byOp(map[string]string{"site.list": siteListJSON("", "", false), "site.vhost": info, "site.vhostset": "saved", "site.vhostreset": "reset", "site.info": phpInfo()})
}

func TestAdvancedTabShowsTheVhostConfigurationAndWhoWroteIt(t *testing.T) {
	h, _, c, _ := setupWith(t, vhostAnswers(false), nil)
	body := do(h, "GET", "/sites/shop/advanced", nil, cookie(c)).Body.String()
	for _, want := range []string{`action="/sites/shop/vhost"`, `name="vhost"`, "server_name example.com;", "Panel tạo", "Lưu và áp dụng"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, "/vhost/reset") || strings.Contains(body, "Cấu hình tự sửa") {
		t.Error("nothing to go back to while mpn makes the file")
	}
	h, _, c, _ = setupWith(t, vhostAnswers(true), nil)
	body = do(h, "GET", "/sites/shop/advanced", nil, cookie(c)).Body.String()
	if !strings.Contains(body, "Cấu hình tự sửa") || !strings.Contains(body, `action="/sites/shop/vhost/reset"`) || !strings.Contains(body, "từ chối các thao tác") {
		t.Errorf("a hand-written configuration says so, warns, and offers the way back")
	}
}

func TestSavingSendsTheTextWithUnixLineBreaks(t *testing.T) {
	h, f, c, tok := setupWith(t, vhostAnswers(false), nil)
	text := "server {\r\n    listen 80;\r\n    server_name example.com;\r\n    return 204;\r\n}\r\n"
	w := followed(h, do(h, "POST", "/sites/shop/vhost", url.Values{"csrf": {tok}, "vhost": {text}}, cookie(c)), c)
	req, ok := lastOp(f, "site.vhostset")
	if !ok || req.Site != "shop" || strings.Contains(req.Vhost, "\r") || !strings.Contains(req.Vhost, "return 204;") {
		t.Fatalf("%+v\n%s", req, w.Body.String())
	}
	if backOf(t, w.Body.String()) != "/sites/shop/advanced" {
		t.Errorf("back: %s", w.Body.String())
	}
}

func TestAConfigurationThatCannotWorkNeverReachesTheHelper(t *testing.T) {
	for name, text := range map[string]string{
		"empty":     "",
		"no server": "listen 80;",
		"too long":  "server {" + strings.Repeat("#", vhost.MaxCustom) + "}",
	} {
		h, f, c, tok := setupWith(t, vhostAnswers(false), nil)
		w := do(h, "POST", "/sites/shop/vhost", url.Values{"csrf": {tok}, "vhost": {text}}, cookie(c))
		if _, sent := lastOp(f, "site.vhostset"); sent || w.Code != 400 || !strings.Contains(w.Body.String(), "Chưa lưu cấu hình") {
			t.Errorf("%s: sent=%v code=%d", name, sent, w.Code)
		}
	}
}

func TestWhenNginxRefusesThePageSaysTheRunningConfigurationStays(t *testing.T) {
	h, _, c, tok := setupWith(t, func(r ctl.Request) (ctl.Response, error) {
		if r.Op == "site.vhostset" {
			return ctl.Response{Output: "nginx: [emerg] unknown directive \"retrun\" in /etc/nginx/sites-enabled/shop.conf:4", Error: "exit status 2"}, nil
		}
		return vhostAnswers(false)(r)
	}, nil)
	body := followed(h, do(h, "POST", "/sites/shop/vhost", url.Values{"csrf": {tok}, "vhost": {"server { retrun 204; }"}}, cookie(c)), c).Body.String()
	if !strings.Contains(body, "giữ nguyên") || !strings.Contains(body, "unknown directive") {
		t.Errorf("a sentence and nginx's own words are both expected:\n%s", body)
	}
}

func TestResetAndAccessRules(t *testing.T) {
	h, f, c, tok := setupWith(t, vhostAnswers(true), nil)
	followed(h, do(h, "POST", "/sites/shop/vhost/reset", url.Values{"csrf": {tok}}, cookie(c)), c)
	if req, ok := lastOp(f, "site.vhostreset"); !ok || req.Site != "shop" {
		t.Errorf("%+v", req)
	}
	if w := do(h, "POST", "/sites/shop/vhost", url.Values{"csrf": {"wrong"}, "vhost": {"server {}"}}, cookie(c)); w.Code != 403 {
		t.Errorf("wrong token: %d", w.Code)
	}
	if w := do(h, "POST", "/sites/nope/vhost", url.Values{"csrf": {tok}, "vhost": {"server {}"}}, cookie(c)); w.Code != 404 {
		t.Errorf("unknown site: %d", w.Code)
	}
}

func TestOtherTabsExplainAHandWrittenConfigurationRefusal(t *testing.T) {
	h, _, c, tok := setupWith(t, func(r ctl.Request) (ctl.Response, error) {
		if r.Op == "site.domain" || r.Op == "site.ssl" {
			return ctl.Response{Output: "custom-vhost: site \"shop\" uses an nginx configuration written by hand", Error: "exit status 2"}, nil
		}
		return vhostAnswers(true)(r)
	}, nil)
	for path, form := range map[string]url.Values{"/sites/shop/domain": {"domain": {"new.example.com"}}, "/sites/shop/ssl/letsencrypt": {}} {
		form.Set("csrf", tok)
		body := followed(h, do(h, "POST", path, form, cookie(c)), c).Body.String()
		if !strings.Contains(body, "Nâng cao") {
			t.Errorf("%s: %s", path, body)
		}
	}
}
