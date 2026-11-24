package store

import (
	"errors"
	"testing"

	"mpn/internal/bgservice"
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
	if err := s.AddSite(phpSite("shop", "shop.example.com")); err != nil {
		t.Fatal(err)
	}
	_, err := s.AddBackgroundService("shop", bgservice.Service{Name: "mise task", Kind: bgservice.Mise, Task: "worker"})
	if err == nil {
		t.Fatal("mise service added to non-mise site")
	}
}
