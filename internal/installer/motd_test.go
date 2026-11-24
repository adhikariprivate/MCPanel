package installer

import (
	"strings"
	"testing"
)

func TestPanelInstallsLoginGreeting(t *testing.T) {
	p, err := Build(panelOpts())
	if err != nil {
		t.Fatal(err)
	}
	out := p.String()
	panel := out[strings.Index(out, "web panel"):]
	// The plan prints a multi-line script by its first line only.
	w := strings.Index(panel, "write "+motdFile)
	off := strings.Index(panel, "run   sh -c set -e ...")
	if w < 0 || off < 0 || w > off {
		t.Fatalf("the greeting must be written before the distribution's is switched off:\n%s", panel)
	}
	// Nothing of the distribution is deleted, so the greeting can be put back.
	if strings.Contains(motdOffScript, "rm -f /etc/update-motd.d") || strings.Contains(motdOffScript, "rm -rf") {
		t.Error("the distribution's greeting must be switched off, not deleted")
	}
	for _, want := range []string{"#!/bin/sh", "01-mpn-panel.conf", "https://github.com/adhikariprivate/MCPanel/tree/main/docs", "mpn version"} {
		if !strings.Contains(MotdScript, want) {
			t.Errorf("greeting lacks %q", want)
		}
	}
	for _, r := range MotdScript {
		if r > 127 {
			t.Fatalf("greeting must be ASCII, found %q", r)
		}
	}
}
