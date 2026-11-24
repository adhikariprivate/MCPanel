package osinfo

import (
	"strings"
	"testing"
)

func TestDefaultsAreOfferedOnEveryRelease(t *testing.T) {
	for _, tg := range Supported() {
		for _, engine := range []string{"mariadb", "mysql", "postgresql"} {
			if !tg.HasSeries(engine, DefaultSeries(engine)) {
				t.Errorf("%s %s: default %s %s is not offered", tg.ID, tg.Version, engine, DefaultSeries(engine))
			}
		}
	}
}

func TestMySQL9IsNeverOffered(t *testing.T) {
	for _, tg := range Supported() {
		for _, engine := range []string{"mariadb", "mysql"} {
			for _, s := range tg.Series(engine) {
				if engine == "mysql" && strings.HasPrefix(s, "9") {
					t.Errorf("%s %s offers MySQL %s", tg.ID, tg.Version, s)
				}
			}
		}
	}
}

func TestSeriesFollowTheRepositories(t *testing.T) {
	deb13 := Target{ID: "debian", Version: "13", Codename: "trixie"}
	u22 := Target{ID: "ubuntu", Version: "22.04", Codename: "jammy"}
	u26 := Target{ID: "ubuntu", Version: "26.04", Codename: "resolute"}
	cases := []struct {
		t              Target
		engine, series string
		want           bool
	}{
		{deb13, "mysql", "8.0", false},     // Oracle publishes no 8.0 for trixie
		{deb13, "mariadb", "10.11", false}, // MariaDB publishes no 10.11 for trixie
		{u26, "mysql", "8.0", false},
		{u22, "mariadb", "10.6", true}, // only Ubuntu 22.04 has 10.6
		{u22, "mariadb", "12.3", false},
		{u22, "mysql", "8.0", true},
		{deb13, "mysql", "8.4", true},
		{deb13, "postgresql", "19", true},
		{u26, "postgresql", "18", true},
	}
	for _, c := range cases {
		if got := c.t.HasSeries(c.engine, c.series); got != c.want {
			t.Errorf("%s %s %s %s: got %v want %v", c.t.ID, c.t.Version, c.engine, c.series, got, c.want)
		}
	}
}

func TestSeriesReturnsACopy(t *testing.T) {
	tg := Target{ID: "debian", Version: "12", Codename: "bookworm"}
	s := tg.Series("mariadb")
	s[0] = "changed"
	if tg.Series("mariadb")[0] == "changed" {
		t.Error("callers must not be able to change the table")
	}
	if tg.Series("postgres") != nil {
		t.Error("unknown engine has no series")
	}
}
