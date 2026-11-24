package installer

import (
	"strings"
	"testing"
)

func TestPHPSteps(t *testing.T) {
	o := opts("mariadb", 1024)
	o.PHPVersions = []string{"7.4", "8.4"}
	p, err := Build(o)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(p), "base packages,journald limit,nginx,nginx pagespeed,mise,php 7.4,8.4,database mariadb"; got != want {
		t.Errorf("steps=%s want %s", got, want)
	}
	s := p.String()
	for _, want := range []string{
		"php7.4-opcache", "php7.4-fpm", "php8.4-fpm",
		"write /etc/php/8.4/fpm/conf.d/99-mpn.ini", "systemctl disable --now php8.4-fpm",
		"sh -c set -eu ... (multi-line script)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "php8.4-opcache") {
		t.Error("opcache is built in from 8.0 and must not be requested as a package")
	}
}

func TestUnknownPHPVersionFails(t *testing.T) {
	o := opts("", 1024)
	o.PHPVersions = []string{"5.6"}
	if _, err := Build(o); err == nil {
		t.Error("php 5.6 must be rejected")
	}
}

func TestSwapReportedSlightlySmallIsEnough(t *testing.T) {
	p, _ := Build(opts("", 1023)) // the kernel shows a 1024MB swap file as 1023MB
	if strings.Contains(names(p), "swap") {
		t.Errorf("no swap step expected: %s", names(p))
	}
}
