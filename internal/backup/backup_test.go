package backup

import (
	"reflect"
	"strings"
	"testing"

	"mpn/internal/installer"
	"mpn/internal/site"
)

func demo() site.Site {
	return site.Site{Name: "demo", Kind: site.Static, User: "demo", Root: "/home/demo/htdocs",
		Hostnames: []string{"demo.example.com"}}
}

func text(p installer.Plan) string { return p.String() }

// A site whose web root is htdocs/public keeps its code and configuration beside public; backup and restore take htdocs,
// and the manifest says so, so that a backup made before 1.1.1 (only the web root) still goes back to the web root.
func TestFilesRootIsHtdocsWhenWebRootIsBelowIt(t *testing.T) {
	s := demo()
	s.Root = "/home/demo/htdocs/public"
	stamp := "20261104-031500"
	cp, err := CreatePlan(s, stamp, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if out := text(cp); !strings.Contains(out, "tar -C /home/demo/htdocs -czf") || !strings.Contains(out, "backup seal") || !strings.Contains(out, "-htdocs") {
		t.Fatalf("backup must archive htdocs:\n%s", out)
	}
	newM := Manifest{Site: "demo", Stamp: stamp, Files: true, Htdocs: true}
	rp, err := RestorePlan(s, newM, true, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if out := text(rp); !strings.Contains(out, "tar -C /home/demo/htdocs -xzpf") {
		t.Errorf("a backup of htdocs must go back to htdocs:\n%s", out)
	}
	oldM := Manifest{Site: "demo", Stamp: stamp, Files: true}
	rp, err = RestorePlan(s, oldM, true, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if out := text(rp); !strings.Contains(out, "tar -C /home/demo/htdocs/public -xzpf") {
		t.Errorf("a backup made before 1.1.1 holds only the web root and must go back there:\n%s", out)
	}
	moved := demo()
	moved.Root = "/srv/www/demo"
	if _, err := RestorePlan(moved, newM, true, nil, ""); err == nil {
		t.Error("a backup of htdocs must be refused once the web root is outside htdocs")
	}
	if got := filesRoot(moved); got != "/srv/www/demo" {
		t.Errorf("a web root outside htdocs is archived as it is, got %q", got)
	}
}

// The manifest of a site whose web root is htdocs itself is unchanged from 1.1.0.
func TestManifestHtdocsFlag(t *testing.T) {
	plain := Manifest{Site: "demo", Stamp: "20261104-031500", Files: true}
	if strings.Contains(plain.String(), "files_root") {
		t.Errorf("no files_root line expected:\n%s", plain.String())
	}
	m := Manifest{Site: "demo", Stamp: "20261104-031500", Files: true, Htdocs: true, DBs: []string{"demo"}}
	got, err := ParseManifest(m.String())
	if err != nil || !reflect.DeepEqual(got, m) {
		t.Fatalf("round trip: got %+v, %v", got, err)
	}
	if _, err := ParseManifest("mpn backup 1\nsite=demo\nstamp=20261104-031500\nfiles_root=../etc"); err == nil {
		t.Error("an unexpected files_root must be rejected")
	}
}

func TestCreatePlanOrderAndSafety(t *testing.T) {
	p, err := CreatePlan(demo(), "20261104-031500", []string{"demo", "demo_shop"}, "/usr/bin/mariadb-dump")
	if err != nil {
		t.Fatal(err)
	}
	out := text(p)
	order := []string{"install -d -m 0700", "mkdir -m 0700 /var/backups/mpn/demo/20261104-031500", "runuser -u demo -- tar",
		"mariadb-dump --single-transaction", "demo_shop.sql", "backup seal -dir /var/backups/mpn/demo/20261104-031500 -site demo -stamp 20261104-031500 -dbname demo -dbname demo_shop"}
	last := -1
	for _, w := range order {
		i := strings.Index(out, w)
		if i < 0 || i < last {
			t.Fatalf("%q missing or out of order in:\n%s", w, out)
		}
		last = i
	}
	if strings.Contains(out, "mkdir -p") {
		t.Error("an existing backup directory must make the plan fail, not be reused")
	}
	if !strings.Contains(out, "gzip -t") {
		t.Error("the archive must be tested after it is written")
	}
}

func TestMixedEngineBackupAndRestorePlans(t *testing.T) {
	stamp := "20261104-031500"
	engines := map[string]string{"demo": "mysql", "demo_pg": "postgresql"}
	p, err := CreatePlanWithEngines(demo(), stamp, []string{"demo", "demo_pg"}, "/usr/bin/mariadb-dump", engines)
	if err != nil {
		t.Fatal(err)
	}
	out := text(p)
	if !strings.Contains(out, "mariadb-dump --single-transaction") || !strings.Contains(out, "pg_dump --clean --if-exists") {
		t.Fatalf("mixed dump plan is incomplete:\n%s", out)
	}
	m := Manifest{Site: "demo", Stamp: stamp, Files: true, DBs: []string{"demo", "demo_pg"}, Engines: engines}
	parsed, err := ParseManifest(m.String())
	if err != nil || parsed.Engine("demo_pg") != "postgresql" || parsed.Engine("demo") != "mysql" {
		t.Fatalf("engine manifest = %+v, %v", parsed, err)
	}
	rp, err := RestorePlanWithEngines(demo(), parsed, false, parsed.DBs, "/usr/bin/mysql", engines)
	if err != nil {
		t.Fatal(err)
	}
	rout := text(rp)
	if !strings.Contains(rout, "/usr/bin/mysql --default-character-set=utf8mb4 demo") || !strings.Contains(rout, "psql -X -v ON_ERROR_STOP=1 --role=demo_pg -d demo_pg") {
		t.Fatalf("mixed restore plan is incomplete:\n%s", rout)
	}
	if _, err := RestorePlanWithEngines(demo(), parsed, false, parsed.DBs, "/usr/bin/mysql", map[string]string{"demo": "mysql", "demo_pg": "mysql"}); err == nil {
		t.Fatal("restore with a different database engine was accepted")
	}
}

func TestLegacyManifestDatabasesDefaultToMySQLFamily(t *testing.T) {
	m, err := ParseManifest("mpn backup 1\nsite=demo\nstamp=20261104-031500\ndb=demo\n")
	if err != nil || m.Engine("demo") != "mysql" {
		t.Fatalf("legacy engine = %q, %v", m.Engine("demo"), err)
	}
}

func TestCreatePlanRejects(t *testing.T) {
	cases := map[string]func() error{
		"bad stamp": func() error { _, e := CreatePlan(demo(), "../etc", nil, ""); return e },
		"other site's db": func() error {
			_, e := CreatePlan(demo(), "20261104-031500", []string{"shop"}, "/usr/bin/mysqldump")
			return e
		},
		"injected db": func() error {
			_, e := CreatePlan(demo(), "20261104-031500", []string{"demo; rm -rf /"}, "/usr/bin/mysqldump")
			return e
		},
		"dump with space": func() error {
			_, e := CreatePlan(demo(), "20261104-031500", []string{"demo"}, "/usr/bin/my dump")
			return e
		},
		"invalid site": func() error {
			s := demo()
			s.User = "root"
			_, e := CreatePlan(s, "20261104-031500", nil, "")
			return e
		},
	}
	for name, f := range cases {
		if f() == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestManifestRoundTripAndRejects(t *testing.T) {
	m := Manifest{Site: "demo", Stamp: "20261104-031500", Files: true, DBs: []string{"demo", "demo_shop"}}
	got, err := ParseManifest(m.String())
	if err != nil || !reflect.DeepEqual(got, m) {
		t.Fatalf("got %+v, %v", got, err)
	}
	for _, bad := range []string{
		"", "other 1\nsite=demo\nstamp=20261104-031500",
		"mpn backup 1\nsite=demo\nstamp=20261104-031500\ndb=a;b",
		"mpn backup 1\nsite=demo\nstamp=20261104-031500\nfiles=../../etc/passwd",
		"mpn backup 1\nsite=demo\nstamp=x",
		"mpn backup 1\nsite=demo\nstamp=20261104-031500\nrun=rm",
		"mpn backup 1\nsite=demo",
	} {
		if _, err := ParseManifest(bad); err == nil {
			t.Errorf("manifest %q should be rejected", bad)
		}
	}
}

func TestRestorePlan(t *testing.T) {
	m := Manifest{Site: "demo", Stamp: "20261104-031500", Files: true, DBs: []string{"demo"}}
	p, err := RestorePlan(demo(), m, true, []string{"demo"}, "/usr/bin/mariadb")
	if err != nil {
		t.Fatal(err)
	}
	out := text(p)
	for _, w := range []string{"runuser -u demo -- tar -C /home/demo/htdocs -xzpf -", "gzip -dc /var/backups/mpn/demo/20261104-031500/demo.sql.gz | /usr/bin/mariadb"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in:\n%s", w, out)
		}
	}
	other := demo()
	other.Name, other.User = "shop", "shop"
	for name, f := range map[string]func() error{
		"another site's backup": func() error { _, e := RestorePlan(other, m, true, nil, ""); return e },
		"db not in backup": func() error {
			_, e := RestorePlan(demo(), m, false, []string{"demo_shop"}, "/usr/bin/mariadb")
			return e
		},
		"nothing chosen": func() error { _, e := RestorePlan(demo(), m, false, nil, ""); return e },
		"files not in backup": func() error {
			n := m
			n.Files = false
			_, e := RestorePlan(demo(), n, true, nil, "")
			return e
		},
	} {
		if f() == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestExpiredAndPrune(t *testing.T) {
	all := []string{"20261101-010000", "20261104-010000", "junk", "20261102-010000", "20261103-010000"}
	if got := Expired(all, 2); !reflect.DeepEqual(got, []string{"20261102-010000", "20261101-010000"}) {
		t.Errorf("got %v", got)
	}
	if Expired(all, 10) != nil || Expired(all, 0) != nil {
		t.Error("nothing should expire when keep is large, and keep < 1 must never delete everything")
	}
	if _, err := PrunePlan("demo", []string{"../.."}); err == nil {
		t.Error("a path-like stamp must be refused")
	}
	p, err := PrunePlan("demo", []string{"20261101-010000"})
	if err != nil || !strings.Contains(text(p), "rm -rf -- /var/backups/mpn/demo/20261101-010000") {
		t.Errorf("%v\n%s", err, text(p))
	}
}
