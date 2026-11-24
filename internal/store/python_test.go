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
	return site.Site{Name: "py", Kind: site.Kind("python"), User: "py", Root: "/home/py/htdocs", Hostnames: []string{"py.example.com"},
		AppPort: 20007, Entry: "app.py", MemMB: 256}
}

func TestPythonSitesAreRefused(t *testing.T) {
	s, _ := open(t)
	if err := s.AddSite(pySite()); err == nil {
		t.Fatal("Python site creation should be disabled")
	}
}

func TestLegacyRuntimeInventoryOnFreshDatabaseIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	sites, err := LegacyRuntimeSites(path)
	if err != nil || len(sites) != 0 {
		t.Fatalf("fresh database inventory: %+v err=%v", sites, err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("fresh database should migrate normally: %v", err)
	}
	s.Close()
}

func TestLegacyRuntimeInventoryIncludesDelegatedAccessIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations[:len(migrations)-1] {
		if _, err := db.Exec(m); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO sites (name,kind,system_user,root_dir,created_at,access_id) VALUES ('legacy','python','legacy','/home/legacy/htdocs',1,'0123456789abcdef0123456789abcdef')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	sites, err := LegacyRuntimeSites(path)
	if err != nil || len(sites) != 1 || sites[0].AccessID != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("legacy inventory identity: %+v err=%v", sites, err)
	}
}

// The breaking migration removes generated runtime sites only after explicit cleanup and keeps their DB records detached.
func TestRuntimeRemovalPreservesSiteFilesAndDatabases(t *testing.T) {
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
		`INSERT INTO databases VALUES ('api_data',2,2)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	if _, err := Open(path); err == nil {
		t.Fatal("ordinary Store.Open must refuse to remove legacy runtime sites")
	}
	legacy, err := LegacyRuntimeSites(path)
	if err != nil || len(legacy) != 1 || legacy[0].Name != "api" || legacy[0].User != "api" {
		t.Fatalf("raw legacy inventory: %+v err=%v", legacy, err)
	}
	s, err := OpenAfterRuntimeCleanup(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	blog, err := s.GetSite("blog")
	if err != nil || !reflect.DeepEqual(blog.Hostnames, []string{"blog.example.com", "www.blog.example.com"}) || !blog.TLS || blog.PHPIni["memory_limit"] != "512M" {
		t.Fatalf("blog after the migration: %+v %v", blog, err)
	}
	if _, err := s.GetSite("api"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("legacy site metadata should be removed after OS cleanup: %v", err)
	}
	d, err := s.GetDatabase("api_data")
	if err != nil || d.Name != "api_data" || d.Site != "" || d.Engine != "mysql" {
		t.Fatalf("database metadata must remain detached: %+v %v", d, err)
	}
	if err := s.AddSite(pySite()); err == nil {
		t.Fatal("a python site cannot be added after the migration")
	}
	if rows, err := s.ListDatabases(""); err != nil || len(rows) != 2 || rows[0].Site != "" || rows[1].Site != "blog" {
		t.Fatalf("detached DB list: %+v %v", rows, err)
	}
	if rows, err := s.ListDatabases("blog"); err != nil || len(rows) != 1 || rows[0].Name != "blog_wp" {
		t.Fatalf("site DB list: %+v %v", rows, err)
	}
	// foreign keys are enforced again afterwards
	if _, err := s.db.Exec(`INSERT INTO hostnames VALUES ('x.example.com', 999, 0)`); err == nil {
		t.Error("foreign keys must be on again after the migration")
	}
}
