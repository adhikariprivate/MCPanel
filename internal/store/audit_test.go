package store

import (
	"testing"
	"time"

	"mpn/internal/audit"
)

func TestTheActivityLogFiltersPagesAndPrunes(t *testing.T) {
	s, _ := open(t)
	day := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	for i, e := range []audit.Entry{
		{IP: "1.1.1.1", Site: "blog", Op: "db.add", Kind: "database", OK: true},
		{IP: "1.1.1.1", Site: "shop", Op: "site.add", Kind: "site", OK: true},
		{IP: "2001:db8::5", Site: "blog", Op: "files.delete", Kind: "files", OK: false, Error: "no"},
		{IP: "9.9.9.9", Op: "login.ok", Kind: "auth", OK: true},
	} {
		e.At = day.Add(time.Duration(i) * time.Hour).Unix()
		if err := s.AddAudit(e); err != nil {
			t.Fatal(err)
		}
	}
	old := audit.Entry{At: day.AddDate(0, 0, -100).Unix(), IP: "1.1.1.1", Op: "logout", Kind: "auth", OK: true}
	if err := s.AddAudit(old); err != nil {
		t.Fatal(err)
	}
	count := func(f audit.Filter) int {
		p, err := s.ListAudit(f, time.UTC)
		if err != nil {
			t.Fatal(err)
		}
		return p.Total
	}
	for _, c := range []struct {
		f    audit.Filter
		want int
	}{
		{audit.Filter{}, 5}, {audit.Filter{Site: "blog"}, 2}, {audit.Filter{Sites: []string{"blog", "shop"}}, 3}, {audit.Filter{IP: "1.1.1."}, 3}, {audit.Filter{IP: "2001:DB8:"}, 1},
		{audit.Filter{Kind: "auth"}, 2}, {audit.Filter{From: "2026-11-20", To: "2026-11-20"}, 4}, {audit.Filter{To: "2026-11-19"}, 1},
	} {
		if got := count(c.f); got != c.want {
			t.Errorf("%+v: %d, want %d", c.f, got, c.want)
		}
	}
	p, _ := s.ListAudit(audit.Filter{Limit: 2, Offset: 1}, time.UTC)
	if len(p.Entries) != 2 || p.Entries[0].Op != "files.delete" || p.Entries[0].OK || p.Entries[0].Error != "no" || p.Total != 5 {
		t.Errorf("newest first, paged: %+v", p)
	}
	n, err := s.PruneAudit(day.AddDate(0, 0, -90))
	if err != nil || n != 1 || count(audit.Filter{}) != 4 {
		t.Errorf("prune: %d %v", n, err)
	}
}
