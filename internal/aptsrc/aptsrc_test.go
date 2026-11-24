package aptsrc

import (
	"strings"
	"testing"

	"mpn/internal/osinfo"
)

var (
	bookworm = osinfo.Target{ID: "debian", Version: "12", Codename: "bookworm"}
	noble    = osinfo.Target{ID: "ubuntu", Version: "24.04", Codename: "noble"}
)

func TestPHPSource(t *testing.T) {
	s := PHP(bookworm)
	want := "Types: deb\nURIs: https://packages.sury.org/php/\nSuites: bookworm\nComponents: main\nSigned-By: /usr/share/keyrings/sury-php.gpg\n"
	if s.Content != want {
		t.Errorf("got:\n%s\nwant:\n%s", s.Content, want)
	}
	if s.Path() != "/etc/apt/sources.list.d/sury-php.sources" {
		t.Errorf("path=%s", s.Path())
	}
}

func TestMariaDBUbuntu(t *testing.T) {
	want := "Types: deb\nURIs: https://mirror.mariadb.org/repo/11.8/ubuntu\nSuites: noble\nComponents: main\nSigned-By: /usr/share/keyrings/mariadb.asc\n"
	s, err := MariaDB(noble, "11.8")
	if err != nil || s.Content != want {
		t.Errorf("got:\n%s\nerr=%v", s.Content, err)
	}
}

func TestMariaDBSeriesMustExistOnTheRelease(t *testing.T) {
	s, err := MariaDB(bookworm, "10.11")
	if err != nil || !strings.Contains(s.Content, "URIs: https://mirror.mariadb.org/repo/10.11/debian\n") {
		t.Errorf("10.11 on Debian 12: %q err=%v", s.Content, err)
	}
	trixie := osinfo.Target{ID: "debian", Version: "13", Codename: "trixie"}
	_, err = MariaDB(trixie, "10.11")
	if err == nil || !strings.Contains(err.Error(), "choose one of: 11.8, 12.3") {
		t.Errorf("the error should list what is offered: %v", err)
	}
}

func TestMySQLSeries(t *testing.T) {
	s, err := MySQL(noble, "8.4")
	if err != nil || s.Content != "Types: deb\nURIs: https://repo.mysql.com/apt/ubuntu\nSuites: noble\nComponents: mysql-8.4-lts\nSigned-By: /usr/share/keyrings/mysql.asc\n" {
		t.Errorf("content=%q err=%v", s.Content, err)
	}
	if s, err := MySQL(noble, "8.0"); err != nil || !strings.Contains(s.Content, "Components: mysql-8.0\n") {
		t.Errorf("8.0 on Ubuntu 24.04: %q err=%v", s.Content, err)
	}
	for _, bad := range []string{"5.7", "9.7", "9.7-lts", "8.4-lts", ""} {
		if _, err := MySQL(noble, bad); err == nil {
			t.Errorf("series %q must be refused", bad)
		}
	}
	trixie := osinfo.Target{ID: "debian", Version: "13", Codename: "trixie"}
	if _, err := MySQL(trixie, "8.0"); err == nil {
		t.Error("the Oracle repository has no MySQL 8.0 for Debian 13")
	}
}

func TestPostgreSQLSourceAndPinnedKey(t *testing.T) {
	s, err := PostgreSQL(noble, "18")
	want := "Types: deb\nURIs: https://apt.postgresql.org/pub/repos/apt\nSuites: noble-pgdg\nComponents: main\nSigned-By: /usr/share/keyrings/postgresql.asc\n"
	if err != nil || s.Content != want || s.KeyFingerprint != "B97B0AFCAA1A47F044F244A07FCC7D46ACCC4CF8" {
		t.Errorf("source=%+v err=%v", s, err)
	}
	if s, err := PostgreSQL(noble, "15"); err == nil || len(s.Content) != 0 {
		t.Errorf("unsupported PostgreSQL series should be rejected: %+v err=%v", s, err)
	}
}
