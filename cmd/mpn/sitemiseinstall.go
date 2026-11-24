package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"

	"mpn/internal/site"
)

// siteMiseInstall is called by systemd as root only for the fixed mise pre-start operation. It installs tools
// from the site's configuration as that site's account. The site cannot choose either path or command.
func siteMiseInstall(args []string) {
	fs := flag.NewFlagSet("site mise-install", flag.ExitOnError)
	db := fs.String("db", defaultDB, "database path")
	name := fs.String("name", "", "mise site name")
	_ = fs.Parse(args)
	requireLinuxRoot()
	if !site.ValidName(*name) {
		fail(fmt.Errorf("invalid site name %q", *name))
	}
	st := openStore(*db)
	defer st.Close()
	s, err := st.GetSite(*name)
	fail(err)
	if s.Kind != site.Mise {
		fail(fmt.Errorf("site %q is not a mise site", s.Name))
	}
	if err := s.Validate(); err != nil {
		fail(err)
	}
	u, err := user.Lookup(s.User)
	fail(err)
	uid, err := strconv.Atoi(u.Uid)
	if err != nil || uid == 0 {
		fail(fmt.Errorf("invalid uid for site account %q", s.User))
	}
	home := "/home/" + s.User
	cmd := exec.Command("runuser", "-u", s.User, "--", "/usr/bin/env",
		"HOME="+home,
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"MISE_DATA_DIR="+site.MiseDataDir(s.Name),
		"MISE_INSTALLS_DIR="+site.MiseInstallsDir(s.Name),
		"MISE_CACHE_DIR="+home+"/.cache/mise",
		"MISE_CONFIG_DIR="+home+"/.config/mise",
		"MISE_TRUSTED_CONFIG_PATHS="+s.Root,
		"/usr/local/bin/mise", "--cd", s.Root, "install")
	cmd.Dir = s.Root
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fail(fmt.Errorf("mise install failed for site %q: %w", s.Name, err))
	}
}
