package installer

import (
	"strings"
	"testing"

	"mpn/internal/distro"
	"mpn/internal/osinfo"
)

func planActionsText(p Plan) string {
	var b strings.Builder
	for _, step := range p {
		for _, action := range step.Actions {
			b.WriteString(action.Path)
			b.WriteString(action.Content)
			b.WriteString(strings.Join(action.Args, " "))
		}
	}
	return b.String()
}

func TestPostgreSQLAPTPlanUsesPinnedPGDGSourceAndSelectedVersion(t *testing.T) {
	p, err := DatabasePlan(optsFor(debian12, "postgresql", "18"))
	if err != nil {
		t.Fatal(err)
	}
	out := planActionsText(p)
	for _, want := range []string{"ACCC4CF8.asc", "B97B0AFCAA1A47F044F244A07FCC7D46ACCC4CF8", "bookworm-pgdg", "postgresql-18", "postgresql-contrib-18"} {
		if !strings.Contains(out, want) {
			t.Errorf("APT PostgreSQL plan lacks %q:\n%s", want, out)
		}
	}
}

func TestPostgreSQLELPlanUsesPinnedPGDGPackagesAndService(t *testing.T) {
	oldFamily := distro.Current()
	distro.SetForTest(distro.EL)
	t.Cleanup(func() { distro.SetForTest(oldFamily) })
	el9 := osinfo.Target{ID: "almalinux", Version: "9", Codename: "el9"}
	p, err := DatabasePlan(optsFor(el9, "postgresql", "18"))
	if err != nil {
		t.Fatal(err)
	}
	out := planActionsText(p)
	for _, want := range []string{"PGDG-RPM-GPG-KEY-RHEL", "D4BF08AE67A0B4C7A1DBCCD240BCA2B408B40D20", "pgdg18", "postgresql18-server", "/var/lib/pgsql/18/data/PG_VERSION", "postgresql-18"} {
		if !strings.Contains(out, want) {
			t.Errorf("EL PostgreSQL plan lacks %q:\n%s", want, out)
		}
	}
}

func TestPostgreSQLDriverIsInstalledForExistingAndFuturePHP(t *testing.T) {
	existing, err := DatabasePlan(optsFor(debian12, "postgresql", "18", "8.4"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(planActionsText(existing), "php8.4-pgsql") {
		t.Fatalf("adding PostgreSQL must add the driver to installed PHP:\n%s", existing)
	}

	o := Options{Target: debian12, Profile: opts("", 1024).Profile, PHPVersions: []string{"8.4"}, PostgreSQL: true}
	future, err := PHPPlan(o)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(planActionsText(future), "php8.4-pgsql") {
		t.Fatalf("future PHP install lacks PostgreSQL driver:\n%s", future)
	}

	oldFamily := distro.Current()
	distro.SetForTest(distro.EL)
	t.Cleanup(func() { distro.SetForTest(oldFamily) })
	el9 := osinfo.Target{ID: "almalinux", Version: "9", Codename: "el9"}
	o = Options{Target: el9, Profile: opts("", 1024).Profile, PHPVersions: []string{"8.4"}, PostgreSQL: true}
	el, err := PHPPlan(o)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(planActionsText(el), "php84-php-pgsql") {
		t.Fatalf("EL PHP install lacks PostgreSQL driver:\n%s", el)
	}
}
