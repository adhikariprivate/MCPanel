package provision

import (
	"strings"
	"testing"

	"mpn/internal/installer"
	"mpn/internal/site"
	"mpn/internal/tune"
)

var opts = Options{Profile: tune.New(tune.Input{RAMMB: 960, CPUs: 1}), AppMemMB: 256}

func base(kind site.Kind) site.Site {
	return site.Site{Name: "demo", Kind: kind, User: "demo", Root: "/home/demo/htdocs", Hostnames: []string{"demo.example.com"}}
}

func names(p installer.Plan) string {
	var n []string
	for _, s := range p {
		n = append(n, s.Name)
	}
	return strings.Join(n, ",")
}

func TestPHPSite(t *testing.T) {
	s := base(site.PHP)
	s.PHPVersion = "8.4"
	p, err := Plan(s, opts)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(p), "account and directories,welcome page,php-fpm pool,nginx vhost"; got != want {
		t.Errorf("steps=%s want %s", got, want)
	}
	out := p.String()
	for _, want := range []string{
		"useradd --create-home --home-dir /home/demo", "mpn site mkroot -user demo -root /home/demo/htdocs",
		"www.conf.disabled", "write /etc/php/8.4/fpm/pool.d/demo.conf", "run   /usr/sbin/php-fpm8.4 -t",
		"systemctl reload-or-restart php8.4-fpm", "write /etc/nginx/sites-enabled/demo.conf", "run   nginx -t",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestPoolIsWrittenBeforeFPMIsReloaded(t *testing.T) {
	s := base(site.PHP)
	s.PHPVersion = "7.4"
	p, _ := Plan(s, opts)
	out := p.String()
	if strings.Index(out, "write /etc/php/7.4/fpm/pool.d/demo.conf") > strings.Index(out, "reload-or-restart php7.4-fpm") {
		t.Errorf("order wrong:\n%s", out)
	}
}

func TestBinarySite(t *testing.T) {
	s := base(site.Binary)
	s.AppPort, s.Entry = 20000, "app"
	p, err := Plan(s, opts)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(p), "account and directories,binary service,nginx vhost"; got != want {
		t.Errorf("steps=%s want %s", got, want)
	}
	out := p.String()
	for _, want := range []string{"write /etc/systemd/system/app-demo.service", "systemctl enable app-demo.service", "systemctl restart app-demo.service"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestStaticSiteHasNoRuntimeStep(t *testing.T) {
	p, err := Plan(base(site.Static), opts)
	if err != nil || names(p) != "account and directories,welcome page,nginx vhost" {
		t.Errorf("steps=%s err=%v", names(p), err)
	}
}

func TestInvalidSiteProducesNoPlan(t *testing.T) {
	s := base(site.PHP)
	s.PHPVersion = "8.4"
	s.User = "demo; rm -rf /"
	if p, err := Plan(s, opts); err == nil || p != nil {
		t.Errorf("unsafe user must be rejected, plan=%v err=%v", p, err)
	}
}
