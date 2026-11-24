package provision

import (
	_ "embed"
	"fmt"
	"path"
	"strings"

	"mpn/internal/installer"
	"mpn/internal/site"
)

//go:embed pages/welcome.html
var welcomeHTML string

// PageMarker is in every welcome page. A page that still has it was not written by a person, so installing an application over it is
// allowed; a file without it is the person's own and is never replaced.
const PageMarker = "mpn-default-page"

// PagesDir holds the welcome pages. It belongs to root; a page is copied from here into the web root by the site's own user, so root
// never writes inside a directory the user controls.
const PagesDir = "/usr/local/share/mpn/pages"

// PHPWelcome is the welcome page of a PHP site: the host and the PHP version are filled in by PHP itself. file is the name the page
// tells the person to replace.
func PHPWelcome(file string) string {
	return strings.NewReplacer(
		"{{HOST}}", `<?= htmlspecialchars($_SERVER['HTTP_HOST'] ?? '', ENT_QUOTES) ?>`,
		"{{RUNTIME}}", `PHP <?= PHP_VERSION ?>`,
		"{{FILE}}", file).Replace(welcomeHTML)
}

func staticPage() string {
	return strings.NewReplacer("{{HOST}}", "", "{{RUNTIME}}", "Website tĩnh (HTML)", "{{FILE}}", "index.html").Replace(welcomeHTML)
}

// DefaultPage returns the name and the content of the welcome page a new site gets, or ok=false when it gets none.
func DefaultPage(s site.Site) (name, content string, ok bool) {
	if s.App != "" {
		return "", "", false
	}
	switch s.Kind {
	case site.Static:
		return "index.html", staticPage(), true
	case site.PHP:
		return "index.php", PHPWelcome("index.php"), true
	}
	return "", "", false
}

// defaultPageScript copies the page into the web root, as the site's user, only when the web root is empty. Anything a person put there
// first stays, and a site that is applied again gets no second page.
func defaultPageScript(user, root, stage, dest string) string {
	return fmt.Sprintf(`root=%[2]s
[ -d "$root" ] && [ -z "$(ls -A "$root")" ] || exit 0
runuser -u %[1]s -- sh -c 'umask 022 && cp "$0" "$1"' %[3]s %[4]s`, user, root, stage, dest)
}

// defaultPageStep is the step that gives a new site its welcome page, or false when the site gets none.
func defaultPageStep(s site.Site) (installer.Step, bool) {
	name, content, ok := DefaultPage(s)
	if !ok {
		return installer.Step{}, false
	}
	stage := path.Join(PagesDir, string(s.Kind)+"-"+name)
	return installer.Step{Name: "welcome page", Actions: []installer.Action{
		installer.Run("install", "-d", "-m", "0755", "-o", "root", "-g", "root", PagesDir),
		installer.WriteFile(stage, content, 0o644),
		installer.Run("sh", "-c", defaultPageScript(s.User, s.Root, stage, path.Join(s.Root, name))),
	}}, true
}
