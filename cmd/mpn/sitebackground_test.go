package main

import (
	"testing"

	"mpn/internal/bgservice"
	"mpn/internal/site"
)

func TestBackgroundLinkTarget(t *testing.T) {
	const link = "/etc/systemd/system/multi-user.target.wants/app-demo-bg-4.service"
	const want = "/etc/systemd/system/app-demo-bg-4.service"
	for _, target := range []string{
		"../app-demo-bg-4.service",
		want,
	} {
		if got := backgroundLinkTarget(link, target); got != want {
			t.Errorf("backgroundLinkTarget(%q, %q) = %q, want %q", link, target, got, want)
		}
	}
}

func TestBackgroundCreationMatchesSiteKindAtHelperBoundary(t *testing.T) {
	cases := []struct {
		name    string
		site    site.Site
		service bgservice.Service
		wantErr bool
	}{
		{"php-matches", site.Site{Kind: site.PHP}, bgservice.Service{Name: "queue", Kind: bgservice.PHP, Command: "artisan queue:work"}, false},
		{"binary-matches", site.Site{Kind: site.Binary}, bgservice.Service{Name: "worker", Kind: bgservice.Binary, Command: "bin/worker"}, false},
		{"binary-on-mise", site.Site{Kind: site.Mise}, bgservice.Service{Name: "worker", Kind: bgservice.Binary, Command: "bin/worker"}, true},
		{"runtime-on-static", site.Site{Kind: site.Static}, bgservice.Service{Name: "worker", Kind: bgservice.Binary, Command: "bin/worker"}, true},
		{"runtime-on-proxy", site.Site{Kind: site.Proxy}, bgservice.Service{Name: "worker", Kind: bgservice.Binary, Command: "bin/worker"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateBackgroundForSite(tc.site, tc.service)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateBackgroundForSite() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
