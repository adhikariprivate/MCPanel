package access

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mpn/internal/site"
)

type call struct{ stdin, name, args string }

type fake struct {
	passwd string // what getent prints; empty means the account does not exist
	fail   string // program that fails
	calls  []call
}

func (f *fake) Run(stdin, name string, args ...string) (string, error) {
	f.calls = append(f.calls, call{stdin, name, strings.Join(args, " ")})
	if name == "getent" {
		if f.passwd == "" {
			return "", errors.New("exit status 2")
		}
		return f.passwd + "\n", nil
	}
	if name == f.fail {
		return "boom " + stdin, errors.New("exit status 1")
	}
	return "", nil
}

func (f *fake) ran(name string) bool {
	for _, c := range f.calls {
		if c.name == name {
			return true
		}
	}
	return false
}

func demo() site.Site {
	return site.Site{Name: "demo", Kind: site.Static, User: "demo", Root: "/home/demo/htdocs", Hostnames: []string{"demo.example.com"}}
}

const marked = "demo:x:1001:1001:mpn site demo:/home/demo:/usr/sbin/nologin"

func TestEnableSetsThePasswordThroughStdinThenTheShell(t *testing.T) {
	f := &fake{passwd: marked}
	if err := Enable(f, demo(), "Correct-Horse-9"); err != nil {
		t.Fatal(err)
	}
	var cp, um *call
	for i := range f.calls {
		switch f.calls[i].name {
		case "chpasswd":
			cp = &f.calls[i]
		case "usermod":
			um = &f.calls[i]
		}
	}
	if cp == nil || cp.stdin != "demo:Correct-Horse-9\n" || cp.args != "" {
		t.Errorf("chpasswd must read user:password from stdin and take no arguments: %+v", cp)
	}
	if um == nil || um.args != "-s /bin/bash demo" {
		t.Errorf("usermod: %+v", um)
	}
	for _, c := range f.calls {
		if strings.Contains(c.args, "Correct-Horse-9") {
			t.Errorf("the password is on a command line: %+v", c)
		}
	}
}

func TestEnableRefusesWhatItDidNotCreate(t *testing.T) {
	for name, passwd := range map[string]string{
		"missing":        "",
		"another site":   "demo:x:1001:1001:mpn site other:/home/demo:/usr/sbin/nologin",
		"no mark":        "demo:x:1001:1001:Some Person:/home/demo:/bin/bash",
		"damaged record": "demo:x",
	} {
		f := &fake{passwd: passwd}
		if Enable(f, demo(), "Correct-Horse-9") == nil || f.ran("chpasswd") || f.ran("usermod") {
			t.Errorf("%s: the account must be left alone (%+v)", name, f.calls)
		}
		g := &fake{passwd: passwd}
		if Disable(g, demo()) == nil || g.ran("usermod") || g.ran("passwd") {
			t.Errorf("%s: disable must leave the account alone", name)
		}
	}
}

func TestEnableChecksThePasswordAndTheSite(t *testing.T) {
	for _, pw := range []string{"", "short", "has space in it", "quote'inside-pass", "back`tick-password", strings.Repeat("a", 65)} {
		f := &fake{passwd: marked}
		if Enable(f, demo(), pw) == nil || len(f.calls) != 0 {
			t.Errorf("password %q accepted", pw)
		}
	}
	bad := demo()
	bad.User = "root"
	if f := (&fake{passwd: marked}); Enable(f, bad, "Correct-Horse-9") == nil || len(f.calls) != 0 {
		t.Error("a system account must be refused before anything runs")
	}
}

func TestAFailureNeverRepeatsThePassword(t *testing.T) {
	f := &fake{passwd: marked, fail: "chpasswd"}
	err := Enable(f, demo(), "Correct-Horse-9")
	if err == nil || strings.Contains(err.Error(), "Correct-Horse-9") {
		t.Errorf("error: %v", err)
	}
	if f.ran("usermod") {
		t.Error("the shell must not be changed when the password could not be set")
	}
}

func TestDisableRemovesTheShellAndLocksThePassword(t *testing.T) {
	f := &fake{passwd: marked}
	if err := Disable(f, demo()); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range f.calls[1:] {
		got = append(got, c.name+" "+c.args)
	}
	if len(got) != 3 || got[0] != "usermod -s /usr/sbin/nologin demo" || got[1] != "passwd -l demo" || !strings.HasPrefix(got[2], "runuser -u demo -- sh -c ") || !strings.HasSuffix(got[2], " sh /home/demo remove") {
		t.Errorf("calls: %v", got)
	}
}

func TestEnabledReadsTheShell(t *testing.T) {
	p := filepath.Join(t.TempDir(), "passwd")
	_ = os.WriteFile(p, []byte("root:x:0:0:root:/root:/bin/bash\na:x:1:1::/home/a:/usr/sbin/nologin\nb:x:2:2::/home/b:/bin/false\nc:x:3:3::/home/c:/bin/bash\nd:x:4:4::/home/d:/bin/sh\n"), 0o644)
	for user, want := range map[string]bool{"a": false, "b": false, "c": true, "d": true, "nobody": false} {
		if Enabled(p, user) != want {
			t.Errorf("%s: want %v", user, want)
		}
	}
	if Enabled(filepath.Join(t.TempDir(), "none"), "c") {
		t.Error("a missing file means no login")
	}
}

func TestEnableGivesTheAccountItsOwnWelcomeInsteadOfTheSystemsText(t *testing.T) {
	f := &fake{passwd: marked}
	if err := Enable(f, demo(), "Correct-Horse-9"); err != nil {
		t.Fatal(err)
	}
	var w *call
	for i := range f.calls {
		if f.calls[i].name == "runuser" {
			w = &f.calls[i]
		}
	}
	if w == nil {
		t.Fatal("no welcome text was written")
	}
	// It must run as the site's own user: root writing into a directory the user controls could be tricked by a symlink.
	if !strings.HasPrefix(w.args, "-u demo -- sh -c ") || !strings.HasSuffix(w.args, " sh /home/demo") {
		t.Errorf("runuser call: %q", w.args)
	}
	for _, want := range []string{"# mpn banner", "case $- in *i*)", "MCPN", "'demo.example.com'", "'/home/demo/htdocs'", "Account %s · web root %s", "Type exit to leave."} {
		if !strings.Contains(w.stdin, want) {
			t.Errorf("the profile text lacks %q:\n%s", want, w.stdin)
		}
	}
	for _, want := range []string{".hushlogin", "# mpn banner", "exit 0"} {
		if !strings.Contains(w.args, want) {
			t.Errorf("the script lacks %q", want)
		}
	}
	if strings.Contains(w.stdin, "Correct-Horse-9") || strings.Contains(w.args, "Correct-Horse-9") {
		t.Error("the password must not reach the welcome text step")
	}
}

func TestAFailureOfTheWelcomeTextDoesNotUndoTheLogin(t *testing.T) {
	f := &fake{passwd: marked, fail: "runuser"}
	if err := Enable(f, demo(), "Correct-Horse-9"); err != nil {
		t.Errorf("the login works without the welcome text, so Enable must still succeed: %v", err)
	}
}

func TestWelcomeTextRefusesAnUnexpectedHome(t *testing.T) {
	for _, home := range []string{"", "/root", "/home/x/../../etc", "/home/with space", "/etc"} {
		f := &fake{passwd: marked}
		if WelcomeText(f, demo(), home) == nil || f.ran("runuser") {
			t.Errorf("home %q accepted", home)
		}
	}
}

const goodLine = `restrict,pty,from="127.0.0.1,::1" ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGRvbnRyZWFsbHljYXJlYWJvdXR0aGlza2V5eHh4 mpn-panel`

func TestInstallPanelKeyRunsAsTheSiteUserWithTheLineOnStdin(t *testing.T) {
	f := &fake{passwd: marked}
	if err := InstallPanelKey(f, demo(), goodLine); err != nil {
		t.Fatal(err)
	}
	last := f.calls[len(f.calls)-1]
	if last.name != "runuser" || !strings.HasPrefix(last.args, "-u demo -- sh -c ") || !strings.HasSuffix(last.args, " sh /home/demo install") || last.stdin != goodLine+"\n" {
		t.Errorf("call: %+v", last)
	}
	for _, want := range []string{".ssh", "chmod 700", "grep -v ' mpn-panel$'", "authorized_keys"} {
		if !strings.Contains(last.args, want) {
			t.Errorf("the script lacks %q", want)
		}
	}
}

func TestInstallPanelKeyRefusesWhatItShouldNot(t *testing.T) {
	for name, line := range map[string]string{
		"empty":              "",
		"no restrictions":    `ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGRvbnRyZWFsbHk mpn-panel`,
		"a command":          `command="rm -rf /",restrict,pty,from="127.0.0.1,::1" ssh-ed25519 AAAAC3Nz mpn-panel`,
		"two lines":          goodLine + "\n" + goodLine,
		"another comment":    strings.Replace(goodLine, "mpn-panel", "someone", 1),
		"not from this host": strings.Replace(goodLine, `from="127.0.0.1,::1"`, `from="*"`, 1),
		"an rsa key":         strings.Replace(goodLine, "ssh-ed25519", "ssh-rsa", 1),
	} {
		f := &fake{passwd: marked}
		if InstallPanelKey(f, demo(), line) == nil || f.ran("runuser") {
			t.Errorf("%s: accepted", name)
		}
	}
	for name, passwd := range map[string]string{"missing": "", "another site": "demo:x:1:1:mpn site other:/home/demo:/bin/bash", "no mark": "demo:x:1:1:Some Person:/home/demo:/bin/bash"} {
		f := &fake{passwd: passwd}
		if InstallPanelKey(f, demo(), goodLine) == nil || f.ran("runuser") || RemovePanelKey(f, demo()) == nil {
			t.Errorf("%s: the account must be left alone", name)
		}
	}
	f := &fake{passwd: "demo:x:1:1:mpn site demo:/etc:/bin/bash"}
	if InstallPanelKey(f, demo(), goodLine) == nil || f.ran("runuser") {
		t.Error("a home directory outside /home must be refused")
	}
}

func TestRemovePanelKeyAsksForRemoveAndSendsNothing(t *testing.T) {
	f := &fake{passwd: marked}
	if err := RemovePanelKey(f, demo()); err != nil {
		t.Fatal(err)
	}
	last := f.calls[len(f.calls)-1]
	if !strings.HasSuffix(last.args, " sh /home/demo remove") || last.stdin != "" {
		t.Errorf("call: %+v", last)
	}
}
