package store

import (
	"errors"
	"reflect"
	"testing"
)

func TestDatabasesBelongToSites(t *testing.T) {
	s, _ := open(t)
	for _, n := range []string{"blog", "shop"} {
		if err := s.AddSite(phpSite(n, n+".example.com")); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range [][2]string{{"blog", "blog"}, {"blog", "blog_stats"}, {"shop", "shop"}} {
		if err := s.AddDatabase(c[0], c[1]); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListDatabases("blog")
	if err != nil || !reflect.DeepEqual(got, []Database{{"blog", "blog", "mysql"}, {"blog_stats", "blog", "mysql"}}) {
		t.Errorf("blog: %+v err=%v", got, err)
	}
	all, _ := s.ListDatabases("")
	if len(all) != 3 {
		t.Errorf("all: %+v", all)
	}
	if none, _ := s.ListDatabases("nosuch"); len(none) != 0 {
		t.Errorf("unknown site has none: %+v", none)
	}
	if d, err := s.GetDatabase("shop"); err != nil || d.Site != "shop" {
		t.Errorf("get: %+v err=%v", d, err)
	}
	if _, err := s.GetDatabase("missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("get missing: %v", err)
	}
}

func TestDatabaseEngineIsStored(t *testing.T) {
	s, _ := open(t)
	if err := s.AddSite(phpSite("blog", "blog.example.com")); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDatabaseEngine("blog", "blog_pg", "postgresql"); err != nil {
		t.Fatal(err)
	}
	d, err := s.GetDatabase("blog_pg")
	if err != nil || d.Engine != "postgresql" {
		t.Fatalf("database = %+v, %v", d, err)
	}
}

func TestDatabaseErrors(t *testing.T) {
	s, _ := open(t)
	if err := s.AddSite(phpSite("blog", "blog.example.com")); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDatabase("nosuch", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown site: %v", err)
	}
	if err := s.AddDatabase("blog", "blog"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDatabase("blog", "blog"); !errors.Is(err, ErrExists) {
		t.Errorf("duplicate: %v", err)
	}
	if err := s.DeleteDatabase("missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete missing: %v", err)
	}
}

// Removing a site record must not orphan its databases: the data would be left on the server with nothing in mpn that
// knows about it.
func TestSiteWithDatabasesCannotBeDeleted(t *testing.T) {
	s, _ := open(t)
	if err := s.AddSite(phpSite("blog", "blog.example.com")); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDatabase("blog", "blog"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSite("blog"); !errors.Is(err, ErrInUse) {
		t.Fatalf("want ErrInUse, got %v", err)
	}
	if _, err := s.GetSite("blog"); err != nil {
		t.Errorf("the refused delete must leave the site alone: %v", err)
	}
	if err := s.DeleteDatabase("blog"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSite("blog"); err != nil {
		t.Errorf("once the databases are gone the site can go: %v", err)
	}
}
