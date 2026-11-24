package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/user"
	"strconv"

	"mpn/internal/distro"
	"mpn/internal/provision"
	"mpn/internal/safedir"
	"mpn/internal/site"
)

// siteMkroot makes the web root of a site, and the folders on the way below htdocs, where nothing is there yet. It is a
// step of the plan of a site (provision.createRootAction), run as root. The home directory belongs to the site user, so
// every folder is opened through the one above it without following links (internal/safedir).
func siteMkroot(args []string) {
	fs := flag.NewFlagSet("site mkroot", flag.ExitOnError)
	name := fs.String("user", "", "the account of the site")
	root := fs.String("root", "", "the web root, /home/<user>/htdocs or a folder below it")
	miseSite := fs.String("mise-site", "", "also create this mise site's private runtime data directories")
	_ = fs.Parse(args)
	requireLinuxRoot()
	if !site.ValidName(*name) {
		fail(fmt.Errorf("invalid account name %q", *name))
	}
	rel, err := provision.RootFolders(*name, *root)
	fail(err)
	uid, gid := lookupIDs(*name, distro.WebUser())
	err = safedir.MkdirAll("/home/"+*name, rel, uid, gid, provision.RootMode)
	if errors.Is(err, safedir.ErrLink) {
		// The user put a link in the tree: it is his to keep, and root must not follow it.
		fmt.Printf("note: a folder on the way to %s is a symbolic link; it is left as it is\n", *root)
		return
	}
	fail(err)
	if *miseSite != "" {
		if !site.ValidName(*miseSite) {
			fail(fmt.Errorf("invalid mise site name %q", *miseSite))
		}
		u, err := user.Lookup(*name)
		fail(err)
		runtimeUID, e1 := strconv.Atoi(u.Uid)
		runtimeGID, e2 := strconv.Atoi(u.Gid)
		if e1 != nil || e2 != nil || runtimeUID == 0 {
			fail(fmt.Errorf("invalid uid or gid for %s", *name))
		}
		for _, dir := range []string{*miseSite + "/installs", *miseSite + "/data"} {
			dataFolders, err := safedir.Split(dir)
			fail(err)
			fail(safedir.MkdirAll("/var/lib/mpn/mise", dataFolders, runtimeUID, runtimeGID, 0o700))
		}
	}
}

// lookupIDs returns the uid of the account and the gid of the group, and fails when either is missing.
func lookupIDs(account, group string) (int, int) {
	u, err := user.Lookup(account)
	fail(err)
	g, err := user.LookupGroup(group)
	fail(err)
	uid, err1 := strconv.Atoi(u.Uid)
	gid, err2 := strconv.Atoi(g.Gid)
	if err1 != nil || err2 != nil || uid == 0 {
		fmt.Fprintln(os.Stderr, "the account or the group has an id mpn cannot use")
		os.Exit(1)
	}
	return uid, gid
}
