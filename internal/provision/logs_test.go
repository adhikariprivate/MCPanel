package provision

import (
	"strings"
	"testing"

	"mpn/internal/installer"
	"mpn/internal/site"
)

func actionsOf(p installer.Plan) []installer.Action {
	var out []installer.Action
	for _, s := range p {
		out = append(out, s.Actions...)
	}
	return out
}

func TestLogsAreRootOwnedAndReadableByTheSitesGroup(t *testing.T) {
	s := base(site.PHP)
	s.PHPVersion = "8.4"
	p, err := Plan(s, opts)
	if err != nil {
		t.Fatal(err)
	}
	out := p.String()
	if !strings.Contains(out, "run   install -d -o root -g demo -m 0750 /var/log/mpn/sites/demo") {
		t.Errorf("log directory must be root:<user> 0750:\n%s", out)
	}
	for _, a := range actionsOf(p) {
		if strings.Contains(strings.Join(a.Args, " ")+a.Path, "/home/demo/logs") {
			t.Errorf("nothing may use a log directory the site user owns: %v %s", a.Args, a.Path)
		}
	}
}

func TestVhostWritesLogsToTheRootOwnedDirectory(t *testing.T) {
	p, err := Plan(base(site.Static), opts)
	if err != nil {
		t.Fatal(err)
	}
	var conf string
	for _, a := range actionsOf(p) {
		if strings.HasSuffix(a.Path, "demo.conf") {
			conf = a.Content
		}
	}
	for _, want := range []string{"access_log /var/log/mpn/sites/demo/access.log;", "error_log /var/log/mpn/sites/demo/error.log;"} {
		if !strings.Contains(conf, want) {
			t.Errorf("missing %q:\n%s", want, conf)
		}
	}
	// the directory must exist before the step that runs nginx -t
	out := p.String()
	if strings.Index(out, "/var/log/mpn/sites/demo") > strings.Index(out, "run   nginx -t") {
		t.Error("the log directory must be created before nginx -t")
	}
}

// Root must never run chmod or install on the web root unconditionally: the site user owns the parent directory and
// can replace the web root with a symlink, which those commands follow.
func TestWebRootIsOnlyCreatedWhenAbsent(t *testing.T) {
	p, err := Plan(base(site.Static), opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range actionsOf(p) {
		if len(a.Args) > 0 && (a.Args[0] == "chmod" || a.Args[0] == "chown" || a.Args[0] == "install") {
			if strings.Contains(strings.Join(a.Args, " "), "/home/demo/htdocs") {
				t.Errorf("unconditional %v on the web root", a.Args)
			}
		}
	}
	// No shell either: a script that tests a path and then changes it can be raced by a link swapped in between.
	if strings.Contains(p.String(), "/home/demo/htdocs &&") || !strings.Contains(p.String(), "run   /usr/local/bin/mpn site mkroot -user demo -root /home/demo/htdocs") {
		t.Errorf("the web root must be made by mpn site mkroot:\n%s", p)
	}
}

func TestTLSPlanCreatesTheLogDirectoryForOlderSites(t *testing.T) {
	p, err := TLSPlan(base(site.Static), []byte("C"), []byte("K"), true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.String(), "install -d -o root -g demo -m 0750 /var/log/mpn/sites/demo") {
		t.Errorf("a site created before the move needs its log directory:\n%s", p)
	}
}

func TestPurgeRemovesTheLogs(t *testing.T) {
	p, err := RemovePlan(base(site.Static), true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.String(), "rm -rf /var/log/mpn/sites/demo") {
		t.Errorf("purge must remove the logs too:\n%s", p)
	}
	p, _ = RemovePlan(base(site.Static), false)
	if strings.Contains(p.String(), "rm -rf /var/log") {
		t.Errorf("logs are kept without purge:\n%s", p)
	}
}
