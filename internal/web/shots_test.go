package web

import (
	"bytes"
	"io"
	"mpn/internal/ctl"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestScreenshots takes pictures of the file manager pages in a real browser, for looking at the design. It runs only
// when MPN_BROWSER (Edge or Chrome) and MPN_SHOT_DIR are set. The browser has no login, so a small proxy adds the session
// cookie; for the dark picture it adds a script of its own, served from the same address so the page's policy allows it.
func TestScreenshots(t *testing.T) {
	browser, out := os.Getenv("MPN_BROWSER"), os.Getenv("MPN_SHOT_DIR")
	if browser == "" || out == "" {
		t.Skip("set MPN_BROWSER and MPN_SHOT_DIR to take screenshots")
	}
	_ = os.MkdirAll(out, 0o755)
	e := startFM(t)
	e.srv.Mon, e.srv.UnitState = demoCollector(time.Now()), fakeUnits
	orig := e.srv.Call
	sslMode := "letsencrypt"
	cloudMode := ""
	appMode := ""
	mailMode := ""
	e.srv.Call = func(r ctl.Request) (ctl.Response, error) { // the overview needs an answer to site.info
		if r.Op == "mail.status" {
			if mailMode == "on" {
				return ctl.Response{OK: true, Output: mailOnJSON}, nil
			}
			return ctl.Response{OK: true, Output: mailOffJSON}, nil
		}
		if r.Op == "site.info" {
			return ctl.Response{OK: true, Output: phpInfo()}, nil
		}
		if r.Op == "site.sslinfo" {
			if sslMode == "" {
				return ctl.Response{OK: true, Output: sslInfoJSON("", 0, false, false, "")}, nil
			}
			return ctl.Response{OK: true, Output: sslInfoJSON(sslMode, 62, true, true, "")}, nil
		}
		if r.Op == "security.status" {
			return ctl.Response{OK: true, Output: secStatusJSON}, nil
		}
		if appMode != "" {
			switch r.Op {
			case "site.list":
				if appMode == "wp" {
					return ctl.Response{OK: true, Output: blogWP}, nil
				}
				return ctl.Response{OK: true, Output: blogPlain}, nil
			case "wp.status":
				return ctl.Response{OK: true, Output: wpStatus}, nil
			case "wp.search":
				return ctl.Response{OK: true, Output: `[{"name":"Yoast SEO","slug":"wordpress-seo","rating":96,"version":"23.0","short_description":"Improve your SEO and get found on the web."}]`}, nil
			}
		}
		if r.Op == "site.list" && sslMode == "" {
			return ctl.Response{OK: true, Output: `[{"name":"blog","kind":"php","user":"blog","hostnames":["blog.example.com"],"detail":"php 8.4","tls":false,"root":"/home/blog/htdocs"}]`}, nil
		}
		if r.Op == "cloud.list" {
			return ctl.Response{OK: true, Output: cloudRemote}, nil
		}
		if r.Op == "cloud.status" {
			if cloudMode == "fetching" {
				return ctl.Response{OK: true, Output: `{"connected":true,"email":"me@example.com","pending":null,"enabled":true,"state":null,"fetch":{"stamp":"20261107-033000","at":` + strconv.FormatInt(time.Now().Unix(), 10) + `,"running":true,"ok":false}}`}, nil
			}
			if cloudMode == "on" {
				return ctl.Response{OK: true, Output: cloudConnected(true, `{"at":`+strconv.FormatInt(time.Now().Add(-3*time.Hour).Unix(), 10)+`,"ok":true,"message":"","uploaded":1,"present":7}`)}, nil
			}
			if cloudMode == "s3" {
				return ctl.Response{OK: true, Output: `{"connected":true,"kind":"s3","where":"S3 Cloudflare: my-backups/server1/mpn-backups","pending":null,"enabled":true,"state":{"at":` + strconv.FormatInt(time.Now().Add(-3*time.Hour).Unix(), 10) + `,"ok":true,"uploaded":1,"present":7}}`}, nil
			}
			return ctl.Response{OK: true, Output: cloudOff}, nil
		}
		if r.Op == "backup.list" {
			return ctl.Response{OK: true, Output: `[{"site":"blog","backup":"20261108-033000","state":"ok","files":true,"databases":["blog"],"size_mb":412.5},{"site":"blog","backup":"20261107-033000","state":"ok","files":true,"databases":["blog"],"size_mb":410.2}]`}, nil
		}
		if r.Op == "panel.sslinfo" {
			return ctl.Response{OK: true, Output: pSSLSelfSigned}, nil
		}
		return orig(r)
	}
	seed := map[string]string{
		"htdocs/index.php": "<?php\necho 'hi';\n$a = [1, 2, 3];\nfunction f($x) { return $x * 2; }\n", "htdocs/style.css": "a{}\n", "htdocs/app.js": "let a=1;\n",
		"htdocs/logo.png": "png", "htdocs/.htaccess": "Options -Indexes\n", "htdocs/wp-config.php": "<?php\n", "htdocs/readme.md": "# Hi\n",
		"htdocs/data.json": "{}\n", "htdocs/backup-2026-11-01-full-site.zip": "zip", "htdocs/notes.txt": "hello", "htdocs/wp-content/index.php": "x",
		"htdocs/uploads/a.jpg": "jpg", "logs/access.log": "line\n", ".ssh/authorized_keys": "key\n",
	}
	for name, body := range seed {
		p := filepath.Join(e.home, filepath.FromSlash(name))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(body), 0o644)
	}
	target, _ := url.Parse(e.url)
	shots := []struct {
		name, path, size string
		dark, phone      bool
		js               string
	}{
		{name: "list", path: "/sites/blog/files?path=htdocs", size: "1280,860"},
		{name: "list-dark", path: "/sites/blog/files?path=htdocs", size: "1280,860", dark: true},
		{name: "list-phone", path: "/sites/blog/files?path=htdocs", size: "420,860", phone: true},
		{name: "empty", path: "/sites/blog/files?path=htdocs/uploads", size: "1280,560"},
		{name: "edit", path: "/sites/blog/files/edit?path=htdocs/index.php", size: "1280,860"},
		{name: "edit-dark", path: "/sites/blog/files/edit?path=htdocs/index.php", size: "1280,860", dark: true},
		{name: "overview", path: "/sites/blog", size: "1280,1500"},
		{name: "overview-dark", path: "/sites/blog", size: "1280,1500", dark: true},
		{name: "overview-phone", path: "/sites/blog", size: "420,2200", phone: true},
		{name: "ssl", path: "/sites/blog/domain", size: "1280,1500"},
		{name: "ssl-phone", path: "/sites/blog/domain", size: "420,2400", phone: true},
		{name: "ssl-none", path: "/sites/blog/domain", size: "1280,1100"},
		{name: "security", path: "/security", size: "1280,2000"},
		{name: "cloud-off", path: "/sites/blog/backups", size: "1280,1400"},
		{name: "cloud-on", path: "/sites/blog/backups", size: "1280,1400"},
		{name: "cloud-s3", path: "/sites/blog/backups", size: "1280,1400"},
		{name: "cloud-fetching", path: "/sites/blog/backups", size: "1280,1500"},
		{name: "cloud-list", path: "/sites/blog/cloud/backups", size: "1280,640"},
		{name: "new-php", path: "/sites/new?kind=php", size: "1280,1100"},
		{name: "app-overview", path: "/sites/blog", size: "1280,2300"},
		{name: "wp-tab", path: "/sites/blog/wordpress?k=plugin&q=seo", size: "1280,1900"},
		{name: "email-off", path: "/email", size: "1280,800"},
		{name: "email-on", path: "/email", size: "1280,2300"},
		{name: "email-on-dark", path: "/email", size: "1280,2300", dark: true},
		{name: "monitor", path: "/monitor?range=24h", size: "1280,1500"},
		{name: "monitor-dark", path: "/monitor?range=24h", size: "1280,1500", dark: true},
		{name: "monitor-phone", path: "/monitor?range=6h", size: "420,2600", phone: true},
		{name: "db", path: "/sites/blog/databases", size: "1280,760"},
		{name: "db-dark", path: "/sites/blog/databases", size: "1280,760", dark: true},
		{name: "db-phone", path: "/sites/blog/databases", size: "420,760", phone: true},
		{name: "menu", path: "/sites/blog/files?path=htdocs", size: "1280,860", js: shotMenu},
		{name: "menu-dark", path: "/sites/blog/files?path=htdocs", size: "1280,860", dark: true, js: shotMenu},
		{name: "menu-blank", path: "/sites/blog/files?path=htdocs", size: "1280,860", js: shotBlank},
		{name: "chmod", path: "/sites/blog/files?path=htdocs", size: "1280,860", js: shotChmod},
		{name: "chmod-dark", path: "/sites/blog/files?path=htdocs", size: "1280,860", dark: true, js: shotChmod},
	}
	for _, s := range shots {
		s := s
		if only := os.Getenv("MPN_SHOT_ONLY"); only != "" && !strings.Contains(","+only+",", ","+s.name+",") {
			continue
		}
		sslMode = "letsencrypt"
		if s.name == "ssl-none" {
			sslMode = ""
		}
		cloudMode = ""
		if s.name == "cloud-on" {
			cloudMode = "on"
		}
		if s.name == "cloud-fetching" {
			cloudMode = "fetching"
		}
		if s.name == "cloud-s3" {
			cloudMode = "s3"
		}
		mailMode = ""
		if strings.HasPrefix(s.name, "email-on") {
			mailMode = "on"
		}
		appMode = ""
		switch s.name {
		case "app-overview":
			appMode = "plain"
		case "wp-tab":
			appMode = "wp"
		}
		px := httputil.NewSingleHostReverseProxy(target)
		px.ModifyResponse = func(r *http.Response) error {
			if csp := r.Header.Get("Content-Security-Policy"); csp != "" {
				r.Header.Set("Content-Security-Policy", strings.Replace(csp, "frame-ancestors 'none'", "frame-ancestors *", 1))
			}
			if (s.dark || s.js != "") && strings.HasPrefix(r.Header.Get("Content-Type"), "text/html") {
				b, _ := io.ReadAll(r.Body)
				b = bytes.Replace(b, []byte("</head>"), []byte(`<script src="/__dark.js"></script></head>`), 1)
				r.Body = io.NopCloser(bytes.NewReader(b))
				r.ContentLength = int64(len(b))
				r.Header.Set("Content-Length", strconv.Itoa(len(b)))
			}
			return nil
		}
		front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/__dark.js":
				w.Header().Set("Content-Type", "text/javascript")
				dark := ""
				if s.dark {
					dark = "document.documentElement.classList.add('dark');"
				}
				// The browser resizes its window to take the picture, and a resize closes the menu, as it should for a person.
				// For the picture only, such events are stopped before the page's own listeners see them.
				keep := ""
				if s.js != "" {
					keep = "['resize','blur'].forEach(function(n){window.addEventListener(n,function(e){e.stopImmediatePropagation()},true)});"
				}
				_, _ = w.Write([]byte(dark + keep + s.js))
				return
			case "/__phone":
				_, _ = w.Write([]byte(`<!doctype html><body style="margin:0;background:#888"><iframe src="` + s.path + `" style="width:390px;height:840px;border:0;display:block;margin:0"></iframe>`))
				return
			}
			r.Header.Set("Cookie", e.cookie)
			if l := os.Getenv("MPN_SHOT_LANG"); l != "" { // MPN_SHOT_LANG=zh takes the pictures in that language
				r.Header.Set("Cookie", e.cookie+"; mpn_language="+l)
			}
			r.Host = target.Host
			px.ServeHTTP(w, r)
		}))
		page := front.URL + s.path
		if s.phone {
			page = front.URL + "/__phone"
		}
		args := []string{"--headless=new", "--disable-gpu", "--no-first-run", "--user-data-dir=" + t.TempDir(), "--hide-scrollbars",
			"--window-size=" + s.size, "--virtual-time-budget=7000", "--screenshot=" + filepath.Join(out, s.name+".png"), page}
		cmd := exec.Command(browser, args...)
		done := make(chan struct{})
		go func() { _, _ = cmd.CombinedOutput(); close(done) }()
		select {
		case <-done:
		case <-time.After(45 * time.Second):
			_ = cmd.Process.Kill()
			t.Errorf("%s: browser timed out", s.name)
		}
		front.Close()
		if _, err := os.Stat(filepath.Join(out, s.name+".png")); err != nil {
			t.Errorf("%s: no picture: %v", s.name, err)
		}
	}
}

// Scripts that bring the page to a state worth a picture: the menu on a file, the menu on the empty part, the dialog.
const shotMenu = `document.addEventListener('DOMContentLoaded', function () {
  var r = document.querySelector('#fm-body tr[data-name="index.php"]');
  r.children[1].dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 330, clientY: 250 }));
});`

const shotBlank = `document.addEventListener('DOMContentLoaded', function () {
  document.getElementById('fm-bar').dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 600, clientY: 400 }));
});`

const shotChmod = `document.addEventListener('DOMContentLoaded', function () {
  var r = document.querySelector('#fm-body tr[data-name="wp-content"]');
  r.children[1].dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 330, clientY: 250 }));
  document.querySelector('#fm-menu [data-m=chmod]').click();
});`
