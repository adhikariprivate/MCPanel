package store

import (
	"errors"
	"testing"

	"mpn/internal/bgservice"
	"mpn/internal/site"
)

func TestBackgroundServiceCRUDIsSiteScopedAndCascades(t *testing.T) {
	s, _ := open(t)
	if err := s.AddSite(phpSite("shop", "shop.example.com")); err != nil {
		t.Fatal(err)
	}
	if err := s.AddSite(phpSite("other", "other.example.com")); err != nil {
		t.Fatal(err)
	}
	v := bgservice.Service{Name: "queue worker", Kind: bgservice.PHP, Command: "artisan queue:work --tries=3", MemoryMB: 256}
	id, err := s.AddBackgroundService("shop", v)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.BackgroundService("shop", id)
	if err != nil || got.ID != id || got.Name != v.Name || got.Kind != v.Kind || got.MemoryMB != v.MemoryMB {
		t.Fatalf("service=%+v err=%v", got, err)
	}
	if _, err := s.BackgroundService("other", id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-site lookup: %v", err)
	}
	v.Name = "queue worker 2"
	if err := s.UpdateBackgroundService("shop", id, v); err != nil {
		t.Fatal(err)
	}
	changedType := bgservice.Service{Name: v.Name, Kind: bgservice.Binary, Command: "bin/worker"}
	if err := s.UpdateBackgroundService("shop", id, changedType); err == nil {
		t.Fatal("background service type changed in place")
	}
	list, err := s.BackgroundServices("shop")
	if err != nil || len(list) != 1 || list[0].Name != v.Name {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	if err := s.DeleteSite("shop"); err != nil {
		t.Fatal(err)
	}
	list, err = s.BackgroundServices("shop")
	if err != nil || len(list) != 0 {
		t.Fatalf("site deletion left services: %+v err=%v", list, err)
	}
}

func TestBackgroundServiceTypeMustMatchSite(t *testing.T) {
	s, _ := open(t)
	sites := []struct {
		value site.Site
		kind  bgservice.Kind
	}{
		{phpSite("shop", "shop.example.com"), bgservice.PHP},
		{site.Site{Name: "binary", Kind: site.Binary, User: "binary", Root: "/home/binary/app", Hostnames: []string{"binary.example.com"}, AppPort: 20000, Entry: "bin/server"}, bgservice.Binary},
		{site.Site{Name: "mise", Kind: site.Mise, User: "mise", Root: "/home/mise/htdocs", Hostnames: []string{"mise.example.com"}, AppPort: 20001}, bgservice.Mise},
		{site.Site{Name: "staticdemo", Kind: site.Static, User: "staticdemo", Root: "/home/staticdemo/htdocs", Hostnames: []string{"static.example.com"}}, ""},
		{site.Site{Name: "proxydemo", Kind: site.Proxy, User: "proxydemo", Root: "/home/proxydemo/htdocs", Hostnames: []string{"proxy.example.com"}, Upstream: "http://127.0.0.1:3000"}, ""},
	}
	for _, item := range sites {
		if err := s.AddSite(item.value); err != nil {
			t.Fatalf("add %s site: %v", item.value.Kind, err)
		}
		services := []bgservice.Service{
			{Name: "php", Kind: bgservice.PHP, Command: "artisan queue:work"},
			{Name: "binary", Kind: bgservice.Binary, Command: "bin/worker"},
			{Name: "mise", Kind: bgservice.Mise, Task: "worker"},
		}
		for _, service := range services {
			_, err := s.AddBackgroundService(item.value.Name, service)
			if service.Kind == item.kind && item.kind != "" {
				if err != nil {
					t.Errorf("%s site rejected matching %s service: %v", item.value.Kind, service.Kind, err)
				}
			} else if err == nil {
				t.Errorf("%s site accepted %s service", item.value.Kind, service.Kind)
			}
		}
	}
}

func TestLegacyMismatchedBackgroundServiceCanBeRemovedButNotEdited(t *testing.T) {
	s, _ := open(t)
	mise := site.Site{Name: "mise", Kind: site.Mise, User: "mise", Root: "/home/mise/htdocs", Hostnames: []string{"mise.example.com"}, AppPort: 20001}
	if err := s.AddSite(mise); err != nil {
		t.Fatal(err)
	}
	var siteID int64
	if err := s.db.QueryRow(`SELECT id FROM sites WHERE name=?`, mise.Name).Scan(&siteID); err != nil {
		t.Fatal(err)
	}
	res, err := s.db.Exec(`INSERT INTO background_services(site_id,name,kind,command,task,memory_mb,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, siteID, "old binary worker", bgservice.Binary, "bin/worker", "", 0, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateBackgroundService(mise.Name, id, bgservice.Service{Name: "renamed", Kind: bgservice.Binary, Command: "bin/worker"}); err == nil {
		t.Fatal("edited a legacy background service whose type does not match the site")
	}
	if err := s.DeleteBackgroundService(mise.Name, id); err != nil {
		t.Fatalf("could not remove legacy mismatched service: %v", err)
	}
}
