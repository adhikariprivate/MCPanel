package installer

import (
	"strings"
	"testing"

	"mpn/internal/osinfo"
	"mpn/internal/tune"
)

var bookworm = osinfo.Target{ID: "debian", Version: "12", Codename: "bookworm"}

func opts(engine string, swap int) Options {
	return Options{Target: bookworm, Profile: tune.New(tune.Input{RAMMB: 1024, CPUs: 1}), CurrentSwapMB: swap, Engine: engine}
}

func names(p Plan) string {
	var n []string
	for _, s := range p {
		n = append(n, s.Name)
	}
	return strings.Join(n, ",")
}

func TestStepOrderWithSwap(t *testing.T) {
	p, err := Build(opts("mariadb", 0))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(p), "base packages,swap,journald limit,nginx,nginx pagespeed,mise,database mariadb"; got != want {
		t.Errorf("steps=%s want %s", got, want)
	}
	if !strings.Contains(p.String(), "run   fallocate -l 1024M /swapfile") {
		t.Errorf("swap size missing:\n%s", p)
	}
}

func TestSwapSkippedWhenEnough(t *testing.T) {
	p, _ := Build(opts("", 1024))
	if got, want := names(p), "base packages,journald limit,nginx,nginx pagespeed,mise"; got != want {
		t.Errorf("steps=%s want %s", got, want)
	}
}

func TestSwapOnlyTopsUp(t *testing.T) {
	p, _ := Build(opts("", 256))
	if !strings.Contains(p.String(), "fallocate -l 768M") {
		t.Errorf("want top-up of 768M:\n%s", p)
	}
}

func TestNginxConfUsesProfile(t *testing.T) {
	c := nginxConf(tune.New(tune.Input{RAMMB: 1024, CPUs: 4}))
	if !strings.Contains(c, "worker_processes 2;") || !strings.Contains(c, "worker_connections 1024;") {
		t.Errorf("conf:\n%s", c)
	}
}

func TestMySQLStep(t *testing.T) {
	p, err := Build(opts("mysql", 1024))
	if err != nil {
		t.Fatal(err)
	}
	s := p.String()
	for _, want := range []string{"mysql-community-server", "/etc/apt/sources.list.d/mysql.sources", "/etc/mysql/conf.d/99-mpn.cnf"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q:\n%s", want, s)
		}
	}
}

func TestBadInputs(t *testing.T) {
	if _, err := Build(opts("postgres", 0)); err == nil {
		t.Error("unknown engine must fail")
	}
	o := opts("mysql", 0)
	o.DBSeries = "5.7"
	if _, err := Build(o); err == nil {
		t.Error("unknown series must fail")
	}
}
