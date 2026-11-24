package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"mpn/internal/site"
)

func pySite() site.Site {
	return site.Site{Name: "py", Kind: site.Python, User: "py", Root: "/home/py/htdocs", Hostnames: []string{"py.example.com"},
		NodePort: 20007, PyVersion: "3.12", Entry: "app.py", MemMB: 256}
}

func TestAPythonSiteIsStoredAndUpdated(t *testing.T) {
	s, _ := open(t)
	if err := s.AddSite(pySite()); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSite("py")
	if err != nil || !reflect.DeepEqual(withoutSiteIdentity(got), pySite()) {
		t.Fatalf("err=%v\ngot  %+v\nwant %+v", err, got, pySite())
	}
	next := pySite()
	next.PyVersion, next.Entry, next.NodePort, next.MemMB = "3.11", "-m myapp", 20008, 0
	if err := s.UpdateSite(next); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetSite("py"); !reflect.DeepEqual(withoutSiteIdentity(got), next) {
		t.Fatalf("after update: %+v", got)
	}
	// the port is unique across node and python sites
	other := pySite()
	other.Name, other.User, other.Hostnames = "py2", "py2", []string{"py2.example.com"}
	other.NodePort = 20008
	if err := s.AddSite(other); !errors.Is(err, ErrExists) {
		t.Errorf("a port in use must be refused: %v", err)
	}
}

// A database made by the last release (version 4) must come through the rebuild of the sites table with its sites,
// hostnames and databases intact: dropping the old table with foreign keys on would wipe the hostnames.
func TestMigrationToPythonKeepsHostnamesAndDatabases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations[:4] {
		if _, err := db.Exec(m); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("PRAGMA user_version = 4"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sites (id, name, kind, system_user, root_dir, php_version, tls, php_ini, mem_mb, created_at) VALUES (1,'blog','php','blog','/home/blog/htdocs','8.4',1,'{"memory_limit":"512M"}',0,1)`,
		`INSERT INTO sites (id, name, kind, system_user, root_dir, app_port, node_version, entry, mem_mb, created_at) VALUES (2,'api','node','api','/home/api/htdocs',20000,'22','server.js',128,1)`,
		`INSERT INTO hostnames VALUES ('blog.example.com',1,0), ('www.blog.example.com',1,1), ('api.example.com',2,0)`,
		`INSERT INTO databases VALUES ('blog_wp',1,1)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	blog, err := s.GetSite("blog")
	if err != nil || !reflect.DeepEqual(blog.Hostnames, []string{"blog.example.com", "www.blog.example.com"}) || !blog.TLS || blog.PHPIni["memory_limit"] != "512M" {
		t.Fatalf("blog after the migration: %+v %v", blog, err)
	}
	api, err := s.GetSite("api")
	if err != nil || api.NodePort != 20000 || api.MemMB != 128 || !reflect.DeepEqual(api.Hostnames, []string{"api.example.com"}) {
		t.Fatalf("api after the migration: %+v %v", api, err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM databases WHERE site_id = 1`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("databases after the migration: %d %v", n, err)
	}
	if err := s.AddSite(pySite()); err != nil {
		t.Fatalf("a python site cannot be added after the migration: %v", err)
	}
	// foreign keys are enforced again afterwards
	if _, err := s.db.Exec(`INSERT INTO hostnames VALUES ('x.example.com', 999, 0)`); err == nil {
		t.Error("foreign keys must be on again after the migration")
	}
}
