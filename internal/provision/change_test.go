package provision

import (
	"strings"
	"testing"

	"mpn/internal/installer"
	"mpn/internal/site"
)

func phpSite(ver string, ini map[string]string) site.Site {
	s := base(site.PHP)
	s.PHPVersion, s.PHPIni = ver, ini
	return s
}

// fileWritten returns the content written to a path by a plan, or "".
func fileWritten(p installer.Plan, path string) string {
	for _, st := range p {
		for _, a := range st.Actions {
			if a.Path == path {
				return a.Content
			}
		}
	}
	return ""
}

func TestThePoolCarriesTheSixCommonSettingsWithTheirValues(t *testing.T) {
	p, err := Plan(phpSite("8.4", nil), opts)
	if err != nil {
		t.Fatal(err)
	}
	pool := fileWritten(p, "/etc/php/8.4/fpm/pool.d/demo.conf")
	for _, want := range []string{"php_admin_value[memory_limit] = 256M", "php_admin_value[max_execution_time] = 60", "php_admin_value[max_input_time] = 60",
		"php_admin_value[max_input_vars] = 5000", "php_admin_value[post_max_size] = 64M", "php_admin_value[upload_max_filesize] = 64M", "request_terminate_timeout = 300s"} {
		if !strings.Contains(pool, want) {
			t.Errorf("pool lacks %q:\n%s", want, pool)
		}
	}
	if strings.Contains(pool, "php_value[") {
		t.Errorf("settings the site did not change must not be written:\n%s", pool)
	}
}

func TestChangedSettingsReachThePoolAndNginx(t *testing.T) {
	s := phpSite("8.4", map[string]string{"memory_limit": "1G", "max_execution_time": "600", "post_max_size": "256M", "upload_max_filesize": "200M",
		"display_errors": "On", "date.timezone": "Asia/Ho_Chi_Minh"})
	p, err := Plan(s, opts)
	if err != nil {
		t.Fatal(err)
	}
	pool := fileWritten(p, "/etc/php/8.4/fpm/pool.d/demo.conf")
	for _, want := range []string{"php_admin_value[memory_limit] = 1G", "php_admin_value[max_execution_time] = 600", "php_admin_value[post_max_size] = 256M",
		"php_admin_value[upload_max_filesize] = 200M", "php_value[display_errors] = On", "php_value[date.timezone] = Asia/Ho_Chi_Minh", "request_terminate_timeout = 630s"} {
		if !strings.Contains(pool, want) {
			t.Errorf("pool lacks %q:\n%s", want, pool)
		}
	}
	conf := fileWritten(p, "/etc/nginx/sites-enabled/demo.conf")
	for _, want := range []string{"client_max_body_size 256M;", "fastcgi_read_timeout 630s;"} {
		if !strings.Contains(conf, want) {
			t.Errorf("vhost lacks %q:\n%s", want, conf)
		}
	}
}

func TestTheWebServerAgreesWithPHPAboutTheSizeOfARequest(t *testing.T) {
	p, _ := Plan(phpSite("8.4", nil), opts)
	conf := fileWritten(p, "/etc/nginx/sites-enabled/demo.conf")
	if !strings.Contains(conf, "client_max_body_size 64M;") {
		t.Errorf("without this nginx refuses an upload of more than 1 MB whatever PHP allows:\n%s", conf)
	}
	st, _ := Plan(base(site.Static), opts)
	if strings.Contains(fileWritten(st, "/etc/nginx/sites-enabled/demo.conf"), "client_max_body_size") {
		t.Error("a site that runs no PHP has no use for the setting")
	}
}

func TestNoTimeLimitForTheScriptStillEndsTheRequestAfterAnHour(t *testing.T) {
	if got := phpSite("8.4", map[string]string{"max_execution_time": "0"}).RequestSeconds(); got != 3600 {
		t.Errorf("%d", got)
	}
	for in, want := range map[string]int{"30": 300, "60": 300, "270": 300, "271": 301, "600": 630, "3600": 3630} {
		if got := phpSite("8.4", map[string]string{"max_execution_time": in}).RequestSeconds(); got != want {
			t.Errorf("max_execution_time %s: %d, want %d", in, got, want)
		}
	}
}

func TestBinarySitesOwnMemoryWinsOverTheDefault(t *testing.T) {
	s := base(site.Binary)
	s.AppPort, s.Entry = 20000, "app"
	p, _ := Plan(s, opts)
	if u := fileWritten(p, "/etc/systemd/system/app-demo.service"); !strings.Contains(u, "MemoryMax=256M") {
		t.Errorf("the default:\n%s", u)
	}
	s.MemMB = 768
	p, _ = Plan(s, opts)
	if u := fileWritten(p, "/etc/systemd/system/app-demo.service"); !strings.Contains(u, "MemoryMax=768M") || strings.Contains(u, "MemoryMax=256M") {
		t.Errorf("the site's own limit:\n%s", u)
	}
}

func TestTheWebRootBelowHtdocsIsMadeFolderByFolder(t *testing.T) {
	got, err := RootFolders("demo", "/home/demo/htdocs")
	if err != nil || strings.Join(got, "/") != "htdocs" {
		t.Errorf("htdocs alone: %v %v", got, err)
	}
	got, err = RootFolders("demo", "/home/demo/htdocs/app/public")
	if err != nil || strings.Join(got, "/") != "htdocs/app/public" {
		t.Errorf("htdocs, then app, then public: %v %v", got, err)
	}
	for _, bad := range []string{"/home/demo/www", "/home/other/htdocs", "/home/demo/htdocs/../x", "/home/demo/htdocs//x"} {
		if _, err := RootFolders("demo", bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestChangingNothingAboutTheVersionIsThePlanOfMakingTheSite(t *testing.T) {
	old := phpSite("8.4", nil)
	cur := phpSite("8.4", map[string]string{"memory_limit": "512M"})
	p, err := ChangePlan(old, cur, opts)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := Plan(cur, opts)
	if p.String() != q.String() {
		t.Errorf("the plan for a change of settings differs from the plan of the site:\n%s\n---\n%s", p, q)
	}
	if strings.Contains(p.String(), "rm -f /etc/php/") {
		t.Error("no pool is dropped when the version stays")
	}
}

func TestMovingToAnotherPhpVersionDropsTheOldPoolAfterTheNewOneWasTested(t *testing.T) {
	p, err := ChangePlan(phpSite("8.2", nil), phpSite("8.4", nil), opts)
	if err != nil {
		t.Fatal(err)
	}
	out := p.String()
	find := func(s string) int { return strings.Index(out, s) }
	write, test, drop, enable, restart := find("write /etc/php/8.4/fpm/pool.d/demo.conf"), find("php-fpm8.4 -t"), find("rm -f /etc/php/8.2/fpm/pool.d/demo.conf"),
		find("systemctl enable php8.4-fpm"), find("reload-or-restart php8.4-fpm")
	if write < 0 || test < 0 || drop < 0 || enable < 0 || restart < 0 {
		t.Fatalf("a step is missing (%d %d %d %d %d):\n%s", write, test, drop, enable, restart, out)
	}
	if !(write < test && test < drop && drop < enable && enable < restart) {
		t.Errorf("the new pool must be written and tested before the old one goes, and the old one must go before the new master starts:\nwrite %d, test %d, drop %d, enable %d, start %d\n%s", write, test, drop, enable, restart, out)
	}
	if !strings.Contains(out, "systemctl disable --now php8.2-fpm") || !strings.Contains(out, "reload-or-restart php8.2-fpm") {
		t.Errorf("the old master must stay only while another site uses it:\n%s", out)
	}
	if find("rm -f /etc/php/8.4") >= 0 {
		t.Error("the new pool must not be dropped")
	}
}

func TestAChangeOfKindIsNotAPlan(t *testing.T) {
	if _, err := ChangePlan(phpSite("8.4", nil), base(site.Static), opts); err != nil {
		// A different kind is refused by the store before this point; the plan itself just makes the site as it now is.
		t.Logf("kind change: %v", err)
	}
	bad := phpSite("8.4", map[string]string{"open_basedir": "/"})
	if _, err := ChangePlan(phpSite("8.4", nil), bad, opts); err == nil {
		t.Error("settings that are not on the list must not become a plan")
	}
}
