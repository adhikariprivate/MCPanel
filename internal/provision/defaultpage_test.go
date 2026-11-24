package provision

import (
	"strings"
	"testing"

	"mpn/internal/site"
)

func pageSite(kind site.Kind, entry string) site.Site {
	s := base(kind)
	s.Entry = entry
	return s
}

func TestEachKindGetsThePageThatFitsIt(t *testing.T) {
	for _, c := range []struct {
		s    site.Site
		name string
		has  string
	}{
		{pageSite(site.Static, ""), "index.html", "<title>Website đã sẵn sàng | MCPN</title>"},
		{pageSite(site.PHP, ""), "index.php", "PHP_VERSION"},
		{pageSite(site.Proxy, ""), "", ""},
	} {
		name, content, ok := DefaultPage(c.s)
		if c.name == "" {
			if ok {
				t.Errorf("%s %q must get no page, got %q", c.s.Kind, c.s.Entry, name)
			}
			continue
		}
		if !ok || name != c.name || !strings.Contains(content, c.has) || !strings.Contains(content, PageMarker) {
			t.Errorf("%s: %q %v\n%s", c.s.Kind, name, ok, content)
		}
	}
}

func TestASiteWithAnInstalledApplicationGetsNoPage(t *testing.T) {
	s := pageSite(site.PHP, "")
	s.App = "wordpress"
	if _, _, ok := DefaultPage(s); ok {
		t.Error("WordPress and the frameworks bring their own index")
	}
}

// The host name comes from the request, so every page must escape it where it is put in.
func TestThePagesEscapeTheHostTheyShow(t *testing.T) {
	_, php, _ := DefaultPage(pageSite(site.PHP, ""))
	if !strings.Contains(php, "htmlspecialchars($_SERVER['HTTP_HOST']") || strings.Contains(php, "{{HOST}}") || strings.Contains(php, "{{RUNTIME}}") {
		t.Errorf("php page:\n%s", php)
	}
	_, static, _ := DefaultPage(pageSite(site.Static, ""))
	if strings.Contains(static, "{{") {
		t.Error("a placeholder is left in the static page")
	}
	if strings.Contains(strings.ToLower(php), "phpinfo") {
		t.Error("the page must not show phpinfo")
	}
}

func TestThePageIsCopiedAsTheSiteUserAndOnlyIntoAnEmptyRoot(t *testing.T) {
	p, err := Plan(pageSite(site.Static, ""), opts)
	if err != nil {
		t.Fatal(err)
	}
	out := p.String()
	for _, want := range []string{"write /usr/local/share/mpn/pages/static-index.html", "multi-line script"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	script := defaultPageScript("demo", "/home/demo/htdocs", PagesDir+"/static-index.html", "/home/demo/htdocs/index.html")
	for _, want := range []string{`[ -z "$(ls -A "$root")" ] || exit 0`, "runuser -u demo --", "umask 022"} {
		if !strings.Contains(script, want) {
			t.Errorf("the copy script lacks %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, " mv ") || strings.Contains(script, "rm ") {
		t.Error("the copy must never delete or move what is there")
	}
}
