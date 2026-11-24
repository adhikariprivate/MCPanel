package ctl

import (
	"reflect"
	"strings"
	"testing"
)

func TestSiteInfoIsAReadAndNeedsAValidSite(t *testing.T) {
	if !Known("site.info") || IsWrite("site.info") {
		t.Error("site.info only reads")
	}
	got, err := Request{Op: "site.info", Site: "blog"}.Argv()
	if err != nil || !reflect.DeepEqual(got, []string{"site", "info", "-name", "blog"}) {
		t.Errorf("%v %v", got, err)
	}
	for _, bad := range []string{"", "Blog", "a b", "-x", "../x", strings.Repeat("a", 40)} {
		if _, err := (Request{Op: "site.info", Site: bad}).Argv(); err == nil {
			t.Errorf("site %q must be refused", bad)
		}
	}
	if !IsWrite("site.set") {
		t.Error("site.set changes the machine: the helper must run it alone")
	}
}

func TestBackgroundServiceOperationsUseFixedHelperCommands(t *testing.T) {
	r := Request{Op: "site.bgadd", Site: "shop", BackgroundName: "queue worker", BackgroundKind: "binary", BackgroundCommand: "bin/worker --queue default"}
	got, err := r.Argv()
	want := []string{"site", "background", "add", "-name", "shop", "-id", "0", "-stdin", "-apply"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("argv=%v err=%v", got, err)
	}
	if !strings.Contains(r.Stdin(), `"kind":"binary"`) || !IsWrite(r.Op) {
		t.Fatalf("stdin=%q write=%t", r.Stdin(), IsWrite(r.Op))
	}
	got, err = Request{Op: "site.bgrestart", Site: "shop", BackgroundID: 7}.Argv()
	if err != nil || !reflect.DeepEqual(got, []string{"site", "background", "restart", "-name", "shop", "-id", "7", "-apply"}) {
		t.Fatalf("argv=%v err=%v", got, err)
	}
	if _, err = (Request{Op: "site.bgstop", Site: "shop"}).Argv(); err == nil {
		t.Fatal("accepted missing service ID")
	}
}

func TestSettingsOfAPhpSiteBecomeFlagsInAFixedOrder(t *testing.T) {
	got, err := Request{Op: "site.set", Site: "blog", Kind: "php", PHP: "8.3", RootSub: "public",
		PHPIni: map[string]string{"memory_limit": "512M", "display_errors": "On", "post_max_size": "128M"}}.Argv()
	want := []string{"site", "set", "-name", "blog", "-root-sub", "public", "-php", "8.3", "-ini-reset",
		"-ini", "display_errors=On", "-ini", "memory_limit=512M", "-ini", "post_max_size=128M", "-apply"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("%v\n%v\n%v", got, want, err)
	}
	// no settings at all means "forget what was set"
	got, _ = Request{Op: "site.set", Site: "blog", Kind: "php", PHP: "8.4"}.Argv()
	if !reflect.DeepEqual(got, []string{"site", "set", "-name", "blog", "-root-sub", "", "-php", "8.4", "-ini-reset", "-apply"}) {
		t.Errorf("%v", got)
	}
}

func TestPhpSettingsThatAreNotOnTheListAreRefused(t *testing.T) {
	base := func(m map[string]string) Request {
		return Request{Op: "site.set", Site: "blog", Kind: "php", PHP: "8.4", PHPIni: m}
	}
	for name, m := range map[string]map[string]string{
		"open_basedir":         {"open_basedir": "/"},
		"disable_functions":    {"disable_functions": ""},
		"auto_prepend_file":    {"auto_prepend_file": "/etc/passwd"},
		"a name with brackets": {"memory_limit]": "1M"},
		"a line break":         {"display_errors": "On\nphp_admin_value[open_basedir] = /"},
		"a semicolon":          {"display_errors": "On;"},
		"memory too large":     {"memory_limit": "9999G"},
		"memory in text":       {"memory_limit": "lots"},
		"time not a number":    {"max_execution_time": "1e9"},
		"time out of range":    {"max_execution_time": "99999"},
		"upload above post":    {"upload_max_filesize": "512M", "post_max_size": "8M"},
		"timezone with spaces": {"date.timezone": "Asia/Ho Chi Minh"},
		"timezone as path":     {"date.timezone": "../../etc/passwd"},
		"error_reporting name": {"error_reporting": "E_ALL"},
	} {
		if _, err := base(m).Argv(); err == nil {
			t.Errorf("%s: %v must be refused", name, m)
		}
	}
	for name, m := range map[string]map[string]string{
		"the six common ones":  {"memory_limit": "512M", "max_execution_time": "120", "max_input_time": "120", "max_input_vars": "10000", "post_max_size": "128M", "upload_max_filesize": "100M"},
		"upload equal to post": {"upload_max_filesize": "64M", "post_max_size": "64M"},
		"timezone":             {"date.timezone": "Asia/Ho_Chi_Minh"},
		"others":               {"display_errors": "On", "error_reporting": "32767", "session.gc_maxlifetime": "7200", "max_file_uploads": "50", "short_open_tag": "On"},
	} {
		if _, err := base(m).Argv(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestSettingsOfOtherKinds(t *testing.T) {
	for name, r := range map[string]Request{
		"node runtime removed":   {Kind: "node"},
		"python runtime removed": {Kind: "python"},
		"root up":                {Kind: "static", RootSub: "../x"},
		"root absolute":          {Kind: "static", RootSub: "/etc"},
		"root hidden":            {Kind: "php", PHP: "8.4", RootSub: ".ssh"},
		"root with space":        {Kind: "php", PHP: "8.4", RootSub: "a b"},
		"root double slash":      {Kind: "php", PHP: "8.4", RootSub: "a//b"},
		"root trailing":          {Kind: "php", PHP: "8.4", RootSub: "a/"},
		"root newline":           {Kind: "php", PHP: "8.4", RootSub: "a\nb"},
		"unknown kind":           {Kind: "ruby"},
		"no kind":                {},
	} {
		r.Op, r.Site = "site.set", "app"
		if _, err := r.Argv(); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	got, err := Request{Op: "site.set", Site: "p", Kind: "proxy", Upstream: "https://127.0.0.1:3000"}.Argv()
	if err != nil || !reflect.DeepEqual(got, []string{"site", "set", "-name", "p", "-upstream", "https://127.0.0.1:3000", "-apply"}) {
		t.Errorf("proxy: %v %v", got, err)
	}
	for _, bad := range []string{"", "ftp://x", "http://", "javascript:1", "http://a;b", "http://a b", "http://a\"b", "http://a$b", "http://a{b}"} {
		if _, err := (Request{Op: "site.set", Site: "p", Kind: "proxy", Upstream: bad}).Argv(); err == nil {
			t.Errorf("upstream %q must be refused", bad)
		}
	}
}
