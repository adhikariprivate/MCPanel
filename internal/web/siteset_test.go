package web

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"mpn/internal/ctl"
)

const nodeList = `[{"name":"app","kind":"node","user":"app","hostnames":["app.example.com"],"detail":"node 22","tls":false,"root":"/home/app/htdocs"}]`
const proxyList = `[{"name":"px","kind":"proxy","user":"px","hostnames":["px.example.com"],"detail":"http://127.0.0.1:3000","tls":false,"root":"/home/px/htdocs"}]`
const staticList = `[{"name":"st","kind":"static","user":"st","hostnames":["st.example.com"],"detail":"","tls":false,"root":"/home/st/htdocs"}]`

func phpInfo() string {
	exp := time.Now().Add(10 * 24 * time.Hour).Unix()
	return fmt.Sprintf(`{"name":"blog","kind":"php","user":"blog","hostnames":["blog.example.com"],"root":"/home/blog/htdocs/public","root_sub":"public","tls":true,"cert_expiry":%d,
"running":true,"run_detail":"php8.4-fpm","disk_bytes":5242880,"last_backup":"20261104-031500","php_version":"8.4",
"php_values":{"memory_limit":"512M","max_execution_time":"60","max_input_time":"60","max_input_vars":"5000","post_max_size":"64M","upload_max_filesize":"64M","display_errors":"On","error_reporting":"24575","date.timezone":"UTC","session.gc_maxlifetime":"1440","max_file_uploads":"20","short_open_tag":"Off"},
"php_set":{"memory_limit":"512M","display_errors":"On"},"php_memory_default":256,"installed_php":["8.3","8.4"],"installed_node":["22"],"nginx":"server { listen 80; }"}`, exp)
}

const nodeInfo = `{"name":"app","kind":"node","user":"app","hostnames":["app.example.com"],"root":"/home/app/htdocs","root_sub":"","tls":false,"running":false,"run_detail":"app-app.service",
"disk_bytes":1000,"node_version":"22","entry":"server.js","port":20003,"mem_mb":0,"installed_php":["8.4"],"installed_node":["20","22"],"nginx":"server {}"}`
const proxyInfo = `{"name":"px","kind":"proxy","user":"px","hostnames":["px.example.com"],"root":"/home/px/htdocs","tls":false,"running":true,"run_detail":"nginx","disk_bytes":0,"upstream":"http://127.0.0.1:3000","installed_php":[],"installed_node":[],"nginx":"server {}"}`
const outsideInfo = `{"name":"st","kind":"static","user":"st","hostnames":["st.example.com"],"root":"/var/www/st","root_outside":true,"tls":false,"running":true,"disk_bytes":10,"installed_php":[],"installed_node":[],"nginx":"server {}"}`

func answersFor(list, info string) func(ctl.Request) (ctl.Response, error) {
	return byOp(map[string]string{"site.list": list, "site.info": info, "site.set": "settings stored", "site.apply": "ok"})
}

func lastSet(f *fakeHelper) (ctl.Request, bool) {
	for i := len(f.reqs) - 1; i >= 0; i-- {
		if f.reqs[i].Op == "site.set" {
			return f.reqs[i], true
		}
	}
	return ctl.Request{}, false
}

func phpForm(extra url.Values) url.Values {
	v := url.Values{"root_sub": {"public"}, "php": {"8.4"}, "ini_memory_limit": {"512M"}, "ini_max_execution_time": {"60"}, "ini_max_input_time": {"60"},
		"ini_max_input_vars": {"5000"}, "ini_post_max_size": {"64M"}, "ini_upload_max_filesize": {"64M"}, "ini_display_errors": {"On"}, "ini_error_reporting": {"24575"},
		"ini_date.timezone": {"UTC"}, "ini_session.gc_maxlifetime": {"1440"}, "ini_max_file_uploads": {"20"}, "ini_short_open_tag": {"Off"}}
	for k, x := range extra {
		v[k] = x
	}
	return v
}

func TestTheOverviewShowsStateInformationAndTheSettingsOfAPhpSite(t *testing.T) {
	h, _, c, _ := setupWith(t, answersFor(blogList, phpInfo()), nil)
	body := do(h, "GET", "/sites/blog", nil, cookie(c)).Body.String()
	for _, want := range []string{"Đang chạy", "php8.4-fpm", "5.0 MB", "còn 9 ngày", "Sắp hết hạn", "Sao lưu gần nhất",
		`action="/sites/blog/settings"`, `name="root_sub" class="field !mt-0 min-w-0 rounded-l-none font-mono" value="public"`, "/home/blog/htdocs/",
		`name="ini_memory_limit"`, `value="512M"`, `name="ini_post_max_size"`, `name="ini_upload_max_filesize"`, `name="ini_max_input_vars"`,
		"đã đổi", "mặc định 256M", "mặc định 64M"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	// only versions that are installed are offered
	for _, want := range []string{`<option>8.3</option>`, `<option selected>8.4</option>`} {
		if !strings.Contains(body, want) {
			t.Errorf("php versions: missing %q", want)
		}
	}
	for _, not := range []string{`<option>7.4</option>`, `<option>8.5</option>`, `name="port"`, `name="upstream"`, `name="node"`} {
		if strings.Contains(body, not) {
			t.Errorf("a php site's overview must not show %q", not)
		}
	}
	if strings.Contains(body, "mpn site") || strings.Contains(body, "password") {
		t.Error("nothing about accounts or passwords on this tab")
	}
}

func TestTheOverviewOfOtherKindsShowsTheirOwnSettings(t *testing.T) {
	h, _, c, _ := setupWith(t, answersFor(nodeList, nodeInfo), nil)
	body := do(h, "GET", "/sites/app", nil, cookie(c)).Body.String()
	for _, want := range []string{"Không chạy", `name="node"`, `<option>20</option>`, `<option selected>22</option>`, `name="entry"`, `value="server.js"`, `name="port"`, `value="20003"`, `name="mem"`} {
		if !strings.Contains(body, want) {
			t.Errorf("node: missing %q", want)
		}
	}
	if strings.Contains(body, `name="ini_memory_limit"`) || strings.Contains(body, `name="upstream"`) {
		t.Error("a node site shows no PHP or proxy settings")
	}
	h, _, c, _ = setupWith(t, answersFor(proxyList, proxyInfo), nil)
	body = do(h, "GET", "/sites/px", nil, cookie(c)).Body.String()
	if !strings.Contains(body, `name="upstream"`) || !strings.Contains(body, `value="http://127.0.0.1:3000"`) || strings.Contains(body, `name="root_sub"`) {
		t.Errorf("proxy:\n%s", body)
	}
}

func TestTheOverviewStillOffersTheActionsWhenTheInformationCannotBeRead(t *testing.T) {
	h, _, c, _ := setupWith(t, func(r ctl.Request) (ctl.Response, error) {
		if r.Op == "site.info" {
			return ctl.Response{Error: "boom"}, nil
		}
		return sitePageAnswers()(r)
	}, nil)
	body := do(h, "GET", "/sites/blog/advanced", nil, cookie(c)).Body.String()
	for _, want := range []string{"boom", `href="/sites/remove?site=blog"`, "blog.example.com"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestSavingPhpSettingsSendsExactlyWhatTheFormHolds(t *testing.T) {
	h, f, c, tok := setupWith(t, answersFor(blogList, phpInfo()), nil)
	form := phpForm(url.Values{"csrf": {tok}, "root_sub": {"/app/public/"}, "php": {"8.3"}, "ini_memory_limit": {"1G"}, "ini_max_execution_time": {"300"}, "ini_date.timezone": {"Asia/Ho_Chi_Minh"},
		"ini_post_max_size": {"128M"}, "ini_upload_max_filesize": {"100M"}, "kind": {"node"}, "user": {"root"}, "name": {"evil"}})
	w := followed(h, do(h, "POST", "/sites/blog/settings", form, cookie(c)), c)
	req, ok := lastSet(f)
	if !ok {
		t.Fatalf("no site.set:\n%s", w.Body.String())
	}
	if req.Site != "blog" || req.Kind != "php" || req.RootSub != "app/public" || req.PHP != "8.3" {
		t.Errorf("identity or location: %+v", req)
	}
	want := map[string]string{"memory_limit": "1G", "max_execution_time": "300", "date.timezone": "Asia/Ho_Chi_Minh", "post_max_size": "128M", "upload_max_filesize": "100M", "display_errors": "On"}
	if len(req.PHPIni) != len(want) {
		t.Errorf("settings: %v want %v", req.PHPIni, want)
	}
	for k, v := range want {
		if req.PHPIni[k] != v {
			t.Errorf("%s = %q, want %q", k, req.PHPIni[k], v)
		}
	}
	// the defaults the machine uses anyway are not stored
	for _, k := range []string{"max_input_time", "max_input_vars", "error_reporting", "session.gc_maxlifetime", "max_file_uploads", "short_open_tag"} {
		if _, there := req.PHPIni[k]; there {
			t.Errorf("%s equals its default and must not be stored", k)
		}
	}
	if backOf(t, w.Body.String()) != "/sites/blog" || !strings.Contains(w.Body.String(), "settings stored") {
		t.Errorf("the result:\n%s", w.Body.String())
	}
}

func TestSettingsAreRefusedBeforeTheHelperIsAskedAndExplainedInVietnamese(t *testing.T) {
	for name, c := range map[string]struct {
		form url.Values
		want string
	}{
		"memory in words":      {url.Values{"ini_memory_limit": {"lots"}}, "memory_limit"},
		"memory too big":       {url.Values{"ini_memory_limit": {"9999G"}}, "memory_limit"},
		"time not a number":    {url.Values{"ini_max_execution_time": {"1e9"}}, "max_execution_time"},
		"timezone with a path": {url.Values{"ini_date.timezone": {"../../etc/passwd"}}, "date.timezone"},
		"upload above post":    {url.Values{"ini_upload_max_filesize": {"512M"}, "ini_post_max_size": {"8M"}}, "post_max_size"},
		"a version not there":  {url.Values{"php": {"7.4"}}, "7.4"},
		"a version not known":  {url.Values{"php": {"9.9"}}, "9.9"},
		"root going up":        {url.Values{"root_sub": {"../etc"}}, "Thư mục con"},
		"root hidden":          {url.Values{"root_sub": {".ssh"}}, "Thư mục con"},
		"root with a space":    {url.Values{"root_sub": {"a b"}}, "Thư mục con"},
	} {
		h, f, ck, tok := setupWith(t, answersFor(blogList, phpInfo()), nil)
		form := phpForm(c.form)
		form.Set("csrf", tok)
		w := do(h, "POST", "/sites/blog/settings", form, cookie(ck))
		if _, sent := lastSet(f); sent {
			t.Errorf("%s: the helper was asked", name)
		}
		body := w.Body.String()
		if w.Code != 400 || !strings.Contains(body, c.want) || !strings.Contains(body, "Chưa lưu cài đặt") || backOf(t, body) != "/sites/blog" {
			t.Errorf("%s: %d\n%s", name, w.Code, body)
		}
	}
}

func TestAWebRootOutsideHtdocsCannotBeChangedHere(t *testing.T) {
	h, f, c, tok := setupWith(t, answersFor(staticList, outsideInfo), nil)
	body := do(h, "GET", "/sites/st", nil, cookie(c)).Body.String()
	if !strings.Contains(body, "nằm ngoài htdocs") || strings.Contains(body, `name="root_sub"`) {
		t.Errorf("the page must say so and offer no field:\n%s", body)
	}
	w := do(h, "POST", "/sites/st/settings", url.Values{"csrf": {tok}, "root_sub": {"public"}}, cookie(c))
	if _, sent := lastSet(f); sent || w.Code != 400 {
		t.Errorf("%d, sent %v", w.Code, sent)
	}
}

func TestSavingNodeAndProxySettings(t *testing.T) {
	h, f, c, tok := setupWith(t, answersFor(nodeList, nodeInfo), nil)
	w := followed(h, do(h, "POST", "/sites/app/settings", url.Values{"csrf": {tok}, "root_sub": {"web"}, "node": {"20"}, "entry": {"dist/main.js"}, "port": {"3000"}, "mem": {"512"}}, cookie(c)), c)
	req, ok := lastSet(f)
	if !ok || req.Kind != "node" || req.Node != "20" || req.Entry != "dist/main.js" || req.Port != 3000 || req.Mem != 512 || req.RootSub != "web" {
		t.Fatalf("%+v\n%s", req, w.Body.String())
	}
	for name, form := range map[string]url.Values{
		"a node version that is not installed": {"node": {"24"}, "entry": {"a.js"}, "port": {"3000"}},
		"an entry that goes up":                {"node": {"22"}, "entry": {"../a.js"}, "port": {"3000"}},
		"the panel's own port":                 {"node": {"22"}, "entry": {"a.js"}, "port": {"8780"}},
		"a port below 1024":                    {"node": {"22"}, "entry": {"a.js"}, "port": {"80"}},
		"a port that is not a number":          {"node": {"22"}, "entry": {"a.js"}, "port": {"abc"}},
		"too little memory":                    {"node": {"22"}, "entry": {"a.js"}, "port": {"3000"}, "mem": {"5"}},
	} {
		h, f, c, tok = setupWith(t, answersFor(nodeList, nodeInfo), nil)
		form.Set("csrf", tok)
		if w := do(h, "POST", "/sites/app/settings", form, cookie(c)); w.Code != 400 {
			t.Errorf("%s: %d", name, w.Code)
		}
		if _, sent := lastSet(f); sent {
			t.Errorf("%s reached the helper", name)
		}
	}
	h, f, c, tok = setupWith(t, answersFor(proxyList, proxyInfo), nil)
	followed(h, do(h, "POST", "/sites/px/settings", url.Values{"csrf": {tok}, "upstream": {"https://127.0.0.1:9000/app"}}, cookie(c)), c)
	if req, ok := lastSet(f); !ok || req.Kind != "proxy" || req.Upstream != "https://127.0.0.1:9000/app" || req.RootSub != "" {
		t.Errorf("%+v", req)
	}
	for _, bad := range []string{"", "ftp://x", "http://a;b", "http://a b", "javascript:1"} {
		h, f, c, tok = setupWith(t, answersFor(proxyList, proxyInfo), nil)
		if w := do(h, "POST", "/sites/px/settings", url.Values{"csrf": {tok}, "upstream": {bad}}, cookie(c)); w.Code != 400 {
			t.Errorf("upstream %q: %d", bad, w.Code)
		}
		if _, sent := lastSet(f); sent {
			t.Errorf("upstream %q reached the helper", bad)
		}
	}
}

func TestSavingNeedsALoginAndTheFormTokenAndAKnownSite(t *testing.T) {
	h, f, c, tok := setupWith(t, answersFor(blogList, phpInfo()), nil)
	if w := do(h, "POST", "/sites/blog/settings", phpForm(url.Values{"csrf": {"wrong"}}), cookie(c)); w.Code != 403 {
		t.Errorf("wrong token: %d", w.Code)
	}
	if w := do(h, "POST", "/sites/blog/settings", phpForm(url.Values{"csrf": {tok}})); w.Code != 303 {
		t.Errorf("no session: %d", w.Code)
	}
	if w := do(h, "GET", "/sites/blog/settings", nil, cookie(c)); w.Code == 200 {
		t.Error("settings are not changed by a plain link")
	}
	for _, name := range []string{"nope", "-x", "A", "a..b"} {
		if w := do(h, "POST", "/sites/"+name+"/settings", phpForm(url.Values{"csrf": {tok}}), cookie(c)); w.Code != 404 {
			t.Errorf("site %q: %d", name, w.Code)
		}
	}
	if _, sent := lastSet(f); sent {
		t.Error("nothing may reach the helper")
	}
}

func TestAHelperFailureWhileSavingIsExplainedAndSaysTheOldConfigurationStays(t *testing.T) {
	h, _, c, tok := setupWith(t, func(r ctl.Request) (ctl.Response, error) {
		switch r.Op {
		case "site.set":
			return ctl.Response{Output: "php 8.3 is not installed on this machine", Error: "exit status 2"}, nil
		case "site.info":
			return ctl.Response{OK: true, Output: phpInfo()}, nil
		}
		return byOp(map[string]string{"site.list": blogList})(r)
	}, nil)
	w := followed(h, do(h, "POST", "/sites/blog/settings", phpForm(url.Values{"csrf": {tok}, "php": {"8.3"}}), cookie(c)), c)
	body := w.Body.String()
	if !strings.Contains(body, "chưa được cài") || !strings.Contains(body, "giữ nguyên") || backOf(t, body) != "/sites/blog" {
		t.Errorf("%s", body)
	}
}
