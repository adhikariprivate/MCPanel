package unit

import (
	"strings"
	"testing"

	"mpn/internal/site"
)

func binarySite() site.Site {
	return site.Site{Name: "api", Kind: site.Binary, User: "api", Root: "/home/api/app",
		Hostnames: []string{"api.example.com"}, AppPort: 20000, Entry: "app"}
}

func TestBinaryUnit(t *testing.T) {
	out, err := Binary(binarySite(), 256)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + out)
	for _, want := range []string{
		"User=api\n", "WorkingDirectory=/home/api/app\n", "Environment=PORT=20000\n",
		"ExecStart=/home/api/app\n", "MemoryMax=256M\n", "Restart=always\n",
		"[Install]\nWantedBy=multi-user.target\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestNoMemoryCapWhenZero(t *testing.T) {
	out, err := Binary(binarySite(), 0)
	if err != nil || strings.Contains(out, "MemoryMax") {
		t.Errorf("err=%v out=%s", err, out)
	}
}

func TestRejectsWrongKindAndBadInput(t *testing.T) {
	php := binarySite()
	php.Kind = site.PHP
	if _, err := Binary(php, 0); err == nil {
		t.Error("php site must be rejected")
	}
	if got := Name(binarySite()); got != "app-api.service" {
		t.Errorf("Name=%q", got)
	}
}
