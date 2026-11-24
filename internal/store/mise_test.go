package store

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"mpn/internal/site"
)

func TestMiseSiteCanBeStoredAndSelected(t *testing.T) {
	s, _ := open(t)
	m := site.Site{Name: "miser", Kind: site.Mise, User: "miser", Root: "/home/miser/htdocs", Hostnames: []string{"mise.example.com"}, NodePort: 20011}
	if err := s.AddSite(m); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSite(m.Name)
	if err != nil || !reflect.DeepEqual(withoutSiteIdentity(got), m) {
		t.Fatalf("site after add: %+v err=%v", got, err)
	}
	m.MiseTask = "serve"
	if err := s.UpdateSite(m); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetSite(m.Name)
	if err != nil || got.MiseTask != "serve" {
		t.Fatalf("site after task selection: %+v err=%v", got, err)
	}
}

func TestMiseMigrationPreservesBinarySitesAndRelations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	previousVersion := schemaVersion - 1
	for _, m := range migrations[:previousVersion] {
		if _, err := db.Exec(m); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("PRAGMA ignore_check_constraints = ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version = " + strconv.Itoa(previousVersion)); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sites (id,name,kind,system_user,root_dir,app_port,entry,mem_mb,created_at) VALUES (4,'gobin','binary','gobin','/home/gobin/htdocs',22004,'bin/server',512,7)`,
		`INSERT INTO hostnames (hostname,site_id,position) VALUES ('go.example.com',4,0)`,
		`INSERT INTO databases (name,site_id,created_at) VALUES ('gobin_data',4,8)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.GetSite("gobin")
	if err != nil || got.Kind != site.Binary || got.NodePort != 22004 || got.Entry != "bin/server" || got.MemMB != 512 || !reflect.DeepEqual(got.Hostnames, []string{"go.example.com"}) {
		t.Fatalf("binary site after migration: %+v err=%v", got, err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM databases WHERE site_id=4`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("related database rows: %d err=%v", n, err)
	}
	m := site.Site{Name: "newmise", Kind: site.Mise, User: "newmise", Root: "/home/newmise/htdocs", Hostnames: []string{"newmise.example.com"}, NodePort: 20012, MiseTask: "serve"}
	if err := s.AddSite(m); err != nil {
		t.Fatalf("mise kind rejected after migration: %v", err)
	}
	if got, err = s.GetSite(m.Name); err != nil || got.MiseTask != "serve" {
		t.Fatalf("mise settings after migration: %+v err=%v", got, err)
	}
	if _, err := s.db.Exec(`INSERT INTO hostnames VALUES ('orphan.example.com',999,0)`); err == nil {
		t.Error("foreign keys must be enabled after migration")
	}
}
