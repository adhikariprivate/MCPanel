package main

import "testing"

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
