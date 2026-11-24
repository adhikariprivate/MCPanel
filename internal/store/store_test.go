package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"mpn/internal/site"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mpn.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func phpSite(name string, hosts ...string) site.Site {
	return site.Site{Name: name, Kind: site.PHP, User: name, Root: "/home/" + name + "/htdocs", Hostnames: hosts, PHPVersion: "8.4"}
}

func withoutSiteIdentity(st site.Site) site.Site {
	st.ID = 0
	st.AccessID = ""
	return st
}

func TestRoundTripAllKinds(t *testing.T) {
	s, _ := open(t)
	want := []site.Site{
		{Name: "api", Kind: site.Binary, User: "api", Root: "/home/api/app", Hostnames: []string{"api.example.com"}, AppPort: 20000, Entry: "bin/server"},
		phpSite("blog", "blog.example.com", "www.blog.example.com"),
		{Name: "docs", Kind: site.Static, User: "docs", Root: "/home/docs/public", Hostnames: []string{"docs.example.com"}},
		{Name: "edge", Kind: site.Proxy, User: "edge", Root: "/home/edge/x", Hostnames: []string{"edge.example.com"}, Upstream: "https://10.0.0.5:8443"},
	}
	for _, st := range []int{3, 1, 0, 2} { // insert out of order, list must come back sorted by name
		if err := s.AddSite(want[st]); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListSites()
	gotWithoutIdentity := make([]site.Site, len(got))
	seenAccessIDs := map[string]bool{}
	for i, st := range got {
		gotWithoutIdentity[i] = withoutSiteIdentity(st)
		if st.ID <= 0 || st.AccessID == "" || seenAccessIDs[st.AccessID] {
			t.Errorf("site identity is missing or duplicated: %+v", st)
		}
		seenAccessIDs[st.AccessID] = true
	}
	if err != nil || !reflect.DeepEqual(gotWithoutIdentity, want) {
		t.Fatalf("err=%v\ngot  %+v\nwant %+v", err, got, want)
	}
	one, err := s.GetSite("blog")
	if err != nil || !reflect.DeepEqual(withoutSiteIdentity(one), want[1]) {
		t.Fatalf("err=%v got %+v", err, one)
	}
}

func TestDuplicatesAreRejectedAtomically(t *testing.T) {
	s, _ := open(t)
	if err := s.AddSite(phpSite("blog", "blog.example.com")); err != nil {
		t.Fatal(err)
	}
	if err := s.AddSite(phpSite("blog", "other.example.com")); !errors.Is(err, ErrExists) {
		t.Errorf("same name: %v", err)
	}
	clash := phpSite("shop", "shop.example.com", "blog.example.com")
	if err := s.AddSite(clash); !errors.Is(err, ErrExists) {
		t.Errorf("same hostname: %v", err)
	}
	if _, err := s.GetSite("shop"); !errors.Is(err, ErrNotFound) {
		t.Errorf("failed insert left a site behind: %v", err)
	}
}

func TestInvalidSiteIsNotStored(t *testing.T) {
	s, _ := open(t)
	bad := phpSite("blog", "a.com; } server {")
	if err := s.AddSite(bad); err == nil {
		t.Fatal("expected validation error")
	}
	if list, _ := s.ListSites(); len(list) != 0 {
		t.Errorf("stored %d sites", len(list))
	}
}

func TestAllocAppPort(t *testing.T) {
	s, _ := open(t)
	app := func(name string, port int) site.Site {
		return site.Site{Name: name, Kind: site.Binary, User: name, Root: "/home/" + name + "/app", Hostnames: []string{name + ".example.com"}, AppPort: port, Entry: "bin/server"}
	}
	for i, port := range []int{20000, 20001, 20003} {
		if err := s.AddSite(app("n"+string(rune('a'+i)), port)); err != nil {
			t.Fatal(err)
		}
	}
	if p, err := s.AllocAppPort(20000, 20010); err != nil || p != 20002 {
		t.Errorf("want gap 20002, got %d err=%v", p, err)
	}
	if err := s.AddSite(app("dup", 20001)); !errors.Is(err, ErrExists) {
		t.Errorf("duplicate port: %v", err)
	}
	if _, err := s.AllocAppPort(20000, 20001); err == nil {
		t.Error("range 20000-20001 is full, expected an error")
	}
}

func TestDeleteFreesHostnames(t *testing.T) {
	s, _ := open(t)
	if err := s.AddSite(phpSite("blog", "blog.example.com")); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSite("blog"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddSite(phpSite("blog2", "blog.example.com")); err != nil {
		t.Errorf("hostname should be free after delete: %v", err)
	}
	if err := s.DeleteSite("missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v", err)
	}
}

func TestPersistsAndVersioned(t *testing.T) {
	s, path := open(t)
	if err := s.AddSite(phpSite("blog", "blog.example.com")); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if _, err := s2.GetSite("blog"); err != nil {
		t.Errorf("site lost after reopen: %v", err)
	}
	var v int
	if err := s2.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil || v != schemaVersion {
		t.Errorf("user_version=%d err=%v", v, err)
	}
	if _, err := s2.db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	s2.Close()
	if _, err := Open(path); err == nil {
		t.Error("a newer schema must be refused")
	}
}
