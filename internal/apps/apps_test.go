package apps

import (
	"regexp"
	"strings"
	"testing"

	"mpn/internal/installer"
	"mpn/internal/site"
)

func phpSite() site.Site {
	return site.Site{Name: "blog", Kind: site.PHP, User: "blog", Root: "/home/blog/htdocs", Hostnames: []string{"blog.example.com"}, PHPVersion: "8.4"}
}

func wpParams() Params {
	r, _ := Get(WordPressID)
	return Params{Site: phpSite(), Recipe: r, DBName: "blog", DBUser: "blog", DBPass: "Zk3hQ8vNp2LwXr7TsB4mYcJd",
		Title: "Blog của tôi", AdminUser: "chusite", AdminPass: "Mk9Wq3Lp7Rt2Xv5Bn8Hd", AdminEmail: "chu@example.com", Locale: "vi"}
}

func fwParams(id string) Params {
	r, _ := Get(id)
	return Params{Site: phpSite(), Recipe: r, DBName: "blog", DBUser: "blog", DBPass: "Zk3hQ8vNp2LwXr7TsB4mYcJd"}
}

// everything lists all arguments and file contents of a plan.
func everything(plans ...installer.Plan) string {
	var b strings.Builder
	for _, p := range plans {
		for _, s := range p {
			for _, a := range s.Actions {
				b.WriteString(strings.Join(a.Args, " ") + "\x00" + a.Path + "\x00" + a.Content + "\x00")
			}
		}
	}
	return b.String()
}

func argsOnly(plans ...installer.Plan) string {
	var b strings.Builder
	for _, p := range plans {
		for _, s := range p {
			for _, a := range s.Actions {
				b.WriteString(strings.Join(a.Args, " ") + "\n")
			}
		}
	}
	return b.String()
}

func TestTheRecipesAreConsistent(t *testing.T) {
	if len(All()) != len(site.Apps) {
		t.Errorf("recipes %v and site.Apps %v must list the same applications", IDs(), site.Apps)
	}
	for _, r := range All() {
		if !site.ValidApp(r.ID) {
			t.Errorf("%s is not in site.Apps", r.ID)
		}
		if r.ID != WordPressID && (r.Package == "") == (r.ZipURL == "") {
			t.Errorf("%s needs exactly one of a Composer package and a release package", r.ID)
		}
		if r.ZipURL != "" && (len(r.ZipSHA256) != 64 || !strings.HasPrefix(r.ZipURL, "https://")) {
			t.Errorf("%s: the release package must be HTTPS with a SHA-256", r.ID)
		}
		if r.DocRoot != "" && !site.ValidSubdir(r.DocRoot) {
			t.Errorf("%s: web root %q is not allowed", r.ID, r.DocRoot)
		}
		if _, _, ok := majorMinor(r.MinPHP); !ok {
			t.Errorf("%s: bad minimum PHP %q", r.ID, r.MinPHP)
		}
		for k, v := range r.PHPIni {
			if _, ok := site.CheckPHPSetting(k, v); !ok {
				t.Errorf("%s: PHP setting %s=%s would be refused", r.ID, k, v)
			}
		}
	}
	if _, ok := Get("nope"); ok {
		t.Error("an unknown application")
	}
}

func TestPHPAtLeast(t *testing.T) {
	for _, c := range []struct {
		have, min string
		want      bool
	}{{"8.4", "8.2", true}, {"8.2", "8.2", true}, {"8.1", "8.2", false}, {"7.4", "7.4", true}, {"7.0", "7.4", false}, {"10.0", "8.4", true}, {"x", "8.2", false}, {"8.4", "", false}} {
		if got := PHPAtLeast(c.have, c.min); got != c.want {
			t.Errorf("PHPAtLeast(%q, %q) = %v", c.have, c.min, got)
		}
	}
}

func TestParamsAreChecked(t *testing.T) {
	if err := wpParams().Check(); err != nil {
		t.Fatal(err)
	}
	bad := map[string]func(*Params){
		"a binary site":      func(p *Params) { p.Site.Kind = site.Binary },
		"already installed":  func(p *Params) { p.Site.App = "laravel" },
		"php too old":        func(p *Params) { p.Recipe, _ = Get("laravel"); p.Site.PHPVersion = "8.1" },
		"no title":           func(p *Params) { p.Title = "" },
		"multi-line title":   func(p *Params) { p.Title = "a\nb" },
		"long title":         func(p *Params) { p.Title = strings.Repeat("x", 101) },
		"bad admin":          func(p *Params) { p.AdminUser = "a b" },
		"short admin":        func(p *Params) { p.AdminUser = "ab" },
		"admin with quote":   func(p *Params) { p.AdminUser = "x';id" },
		"bad email":          func(p *Params) { p.AdminEmail = "nobody" },
		"email with newline": func(p *Params) { p.AdminEmail = "a@example.com\nBcc: x@y.z" },
		"unknown locale":     func(p *Params) { p.Locale = "xx_XX" },
		"short password":     func(p *Params) { p.AdminPass = "short" },
		"password newline":   func(p *Params) { p.AdminPass = "aaaaaaaaaaaa\nbbbb" },
		"database name":      func(p *Params) { p.DBName = "a'; DROP" },
		"database password":  func(p *Params) { p.DBPass = "pa ss" },
		"no database":        func(p *Params) { p.DBName = "" },
	}
	for name, mut := range bad {
		p := wpParams()
		mut(&p)
		if p.Check() == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

// The password of the administrator and of the database are secrets: other accounts of the machine can read the arguments of a process.
// WP-CLI and Composer start processes of their own; started from /root, where the account of a site cannot go, that fails with
// "Permission denied" (found on a real server). Every program run as the site's user therefore starts in the web files.
var chdirRe = regexp.MustCompile(`env --chdir='?/home/blog/htdocs'? `)

func TestEveryProgramRunAsTheSiteUserStartsInItsWebFiles(t *testing.T) {
	for _, p := range []Params{wpParams(), fwParams("laravel")} {
		prep, install, err := Plan(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, plan := range []installer.Plan{prep, install} {
			for _, s := range plan {
				for _, a := range s.Actions {
					line := strings.Join(a.Args, " ")
					runs := strings.Contains(line, "runuser") && (strings.Contains(line, "wp-cli.phar") || strings.Contains(line, "composer.phar"))
					if runs && !chdirRe.MatchString(line) {
						t.Errorf("%s: %s does not start in the web files", p.Recipe.ID, line)
					}
				}
			}
		}
	}
	if j := strings.Join(WPArgv(phpSite(), "plugin", "list"), " "); !strings.Contains(j, "env --chdir=/home/blog/htdocs") {
		t.Errorf("the manager runs WP-CLI the same way: %s", j)
	}
}

func TestNoSecretIsEverAnArgument(t *testing.T) {
	for _, p := range []Params{wpParams(), fwParams("laravel"), fwParams("codeigniter")} {
		prep, install, err := Plan(p)
		if err != nil {
			t.Fatalf("%s: %v", p.Recipe.ID, err)
		}
		args := argsOnly(prep, install)
		for _, secret := range []string{p.DBPass, p.AdminPass} {
			if secret != "" && strings.Contains(args, secret) {
				t.Errorf("%s: a secret is on a command line:\n%s", p.Recipe.ID, args)
			}
		}
	}
}

func TestWordPressStepsAndTheFilesTheySend(t *testing.T) {
	prep, install, err := Plan(wpParams())
	if err != nil {
		t.Fatal(err)
	}
	all := everything(prep, install)
	for _, want := range []string{"wp-cli-" + WPCLIVersion + ".phar", "core download --locale=vi", "core install", "--prompt=admin_password", "--skip-email", "rewrite structure",
		"/usr/bin/php8.4", "runuser -u blog"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q", want)
		}
	}
	// the title is a Vietnamese phrase with spaces and an accent: it must be quoted in the one shell line it appears in
	if !strings.Contains(all, "--title='Blog của tôi'") {
		t.Errorf("the title must be quoted:\n%s", argsOnly(install))
	}
	var cfg, pass string
	for _, s := range install {
		for _, a := range s.Actions {
			switch a.Path {
			case StageDir + "/wp-config.php":
				cfg = a.Content
				if a.Mode != 0o600 {
					t.Errorf("wp-config.php is staged with mode %o", a.Mode)
				}
			case StageDir + "/wp-admin-pass":
				pass = a.Content
			}
		}
	}
	if !strings.Contains(cfg, "define( 'DB_PASSWORD', 'Zk3hQ8vNp2LwXr7TsB4mYcJd' );") || !strings.Contains(cfg, "DISALLOW_FILE_EDIT") {
		t.Errorf("wp-config.php:\n%s", cfg)
	}
	if pass != "Mk9Wq3Lp7Rt2Xv5Bn8Hd\n" {
		t.Errorf("the administrator password goes to WP-CLI on standard input: %q", pass)
	}
	// the staged secrets are removed again
	if !strings.Contains(argsOnly(install), "rm -f '/usr/local/share/mpn/stage/wp-admin-pass'") {
		t.Error("the staged password must be deleted")
	}
}

func TestFrameworkStepsPutTheDatabaseInTheEnvFile(t *testing.T) {
	_, install, err := Plan(fwParams("laravel"))
	if err != nil {
		t.Fatal(err)
	}
	args := argsOnly(install)
	for _, want := range []string{"create-project", "laravel/laravel", "--no-dev", "migrate --force"} {
		if !strings.Contains(args, want) {
			t.Errorf("laravel: missing %q:\n%s", want, args)
		}
	}
	var env string
	for _, s := range install {
		for _, a := range s.Actions {
			if a.Path == StageDir+"/env-block" {
				env = a.Content
			}
		}
	}
	for _, want := range []string{"APP_ENV=production", "APP_DEBUG=false", "APP_URL=http://blog.example.com", "DB_DATABASE=blog", "DB_PASSWORD=Zk3hQ8vNp2LwXr7TsB4mYcJd"} {
		if !strings.Contains(env, want) {
			t.Errorf("laravel .env block lacks %q:\n%s", want, env)
		}
	}
	_, ci, _ := Plan(fwParams("codeigniter"))
	if !strings.Contains(argsOnly(ci), "codeigniter4/appstarter") || !strings.Contains(everything(ci), "CI_ENVIRONMENT = production") {
		t.Error("codeigniter")
	}
	_, yii, _ := Plan(fwParams("yii"))
	if !strings.Contains(argsOnly(yii), "YII_DEBUG") || strings.Contains(everything(yii), "DB_PASSWORD") {
		t.Errorf("yii gets production mode and no database:\n%s", argsOnly(yii))
	}
}

// The skeletons of Yii and CakePHP start in debug mode; CakePHP then loads DebugKit, which is not installed (found on a real server: a 500).
func TestEverySkeletonThatStartsInDebugModeIsPutInProductionMode(t *testing.T) {
	for id, want := range map[string]string{"cakephp": "config/app_local.php", "yii": "YII_DEBUG"} {
		_, install, err := Plan(fwParams(id))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(argsOnly(install), want) {
			t.Errorf("%s: not put in production mode:\n%s", id, argsOnly(install))
		}
	}
}

func TestFrameworksWithoutADatabaseNeedNone(t *testing.T) {
	for _, id := range IDs() {
		r, _ := Get(id)
		want := id == WordPressID || id == "laravel" || id == "codeigniter" || id == PHPBBID
		if r.NeedsDB() != want {
			t.Errorf("%s NeedsDB = %v", id, r.NeedsDB())
		}
	}
	p := fwParams("yii")
	p.DBName, p.DBUser, p.DBPass = "", "", ""
	if err := p.Check(); err != nil {
		t.Errorf("yii has no database: %v", err)
	}
}

func TestOnlyAnEmptyWebRootIsUsedAndOnlyOurPageIsRemoved(t *testing.T) {
	prep, _, err := Plan(wpParams())
	if err != nil {
		t.Fatal(err)
	}
	script := ""
	for _, s := range prep {
		if s.Name == "web root" {
			script = s.Actions[0].Args[2]
		}
	}
	for _, want := range []string{`[ -z "$(ls -A)" ]`, "app-notempty", "grep -q mpn-default-page", "runuser -u blog"} {
		if !strings.Contains(script, want) {
			t.Errorf("the web-root check lacks %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "rm -rf") {
		t.Error("preparing must never delete a person's files")
	}
}

func TestACleanupRemovesTheSecretsAndWhatWasDownloaded(t *testing.T) {
	c := argsOnly(CleanupPlan(wpParams()))
	for _, want := range []string{"wp-config.php", "wp-admin-pass", "env-block", "find . -mindepth 1 -delete"} {
		if !strings.Contains(c, want) {
			t.Errorf("missing %q:\n%s", want, c)
		}
	}
}

func TestToolsAreInstalledOnlyWithTheirPinnedChecksum(t *testing.T) {
	for _, r := range All() {
		if r.ZipURL != "" {
			continue // the package itself is checked in zipSteps (TestPHPBB)
		}
		s := ToolsStep(r)
		script := s.Actions[0].Args[2]
		want := WPCLISHA512
		if r.ID != WordPressID {
			want = ComposerSHA256
		}
		if !strings.Contains(script, want) || !strings.Contains(script, "does not match its checksum; not installed") {
			t.Errorf("%s: the download is not checked:\n%s", r.ID, script)
		}
	}
	if len(WPCLISHA512) != 128 || len(ComposerSHA256) != 64 {
		t.Error("the pinned checksums have the wrong length")
	}
}

func TestExtensionsAreInstalledForTheSitesOwnPHP(t *testing.T) {
	step, ok := ExtensionsStep(fwParams("codeigniter"))
	if !ok || !strings.Contains(argsOnly(installer.Plan{step}), "php8.4-intl") || !strings.Contains(argsOnly(installer.Plan{step}), "php8.4-fpm") {
		t.Errorf("%v %v", ok, step)
	}
	if _, ok := ExtensionsStep(fwParams("laravel")); ok {
		t.Error("laravel needs no extra extension")
	}
}

func TestWPConfigAndSalts(t *testing.T) {
	a, err := Salt()
	if err != nil || len(a) != 64 || strings.ContainsAny(a, `'"\`+"`") {
		t.Errorf("salt %q %v", a, err)
	}
	b, _ := Salt()
	if a == b {
		t.Error("salts must differ")
	}
	cfg, err := WPConfig("blog", "blog", "Zk3hQ8vNp2LwXr7TsB4mYcJd")
	if err != nil || strings.Count(cfg, "_KEY'")+strings.Count(cfg, "_SALT'") != 8 {
		t.Errorf("%v\n%s", err, cfg)
	}
	for _, bad := range []string{"a'b", "a b", `a\b`, "a\nb", ""} {
		if _, err := WPConfig(bad, "u", "p"); err == nil {
			t.Errorf("%q must not go into wp-config.php", bad)
		}
		if _, err := WPConfig("d", "u", bad); err == nil {
			t.Errorf("password %q must not go into wp-config.php", bad)
		}
	}
}
