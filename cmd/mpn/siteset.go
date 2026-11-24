package main

import (
	"errors"
	"flag"
	"fmt"
	"maps"
	"slices"
	"strings"

	"mpn/internal/provision"
	"mpn/internal/site"
	"mpn/internal/tune"
)

// iniList collects "-ini name=value" flags.
type iniList []string

func (l *iniList) String() string     { return strings.Join(*l, ",") }
func (l *iniList) Set(v string) error { *l = append(*l, v); return nil }

// siteSet changes the settings of a site that already exists: the web root below htdocs, the PHP version and settings,
// the version and settings of an app, and the address a proxy forwards to. Only the flags that are
// given change anything, and a flag that does not fit the kind of the site is an error rather than ignored.
//
// With -apply the files of the site are made again, and the database is updated only after that worked, so the stored
// settings always describe what is running. If making the files fails, the executor puts back the files it had written.
func siteSet(args []string) {
	fs := flag.NewFlagSet("site set", flag.ExitOnError)
	db := fs.String("db", defaultDB, "database path")
	name := fs.String("name", "", "site name")
	rootSub := fs.String("root-sub", "", "web root below htdocs, for example public; empty is htdocs itself")
	php := fs.String("php", "", "PHP version, php sites only")
	var ini iniList
	fs.Var(&ini, "ini", "PHP setting as name=value, repeat for several, php sites only")
	iniReset := fs.Bool("ini-reset", false, "forget the PHP settings that were set before, php sites only; with -ini the new ones follow")
	entry := fs.String("entry", "", "binary path for a binary site")
	port := fs.Int("port", 0, "application port for binary or mise sites")
	mem := fs.Int("mem", 0, "memory the app may use in MB, 0 for the default")
	miseTask := fs.String("mise-task", "", "mise task to supervise; empty leaves the app stopped")
	upstream := fs.String("upstream", "", "http(s) URL, proxy sites only")
	apply := fs.Bool("apply", false, "make the changes on this machine (Linux, root); otherwise the plan is only printed")
	r := addRenderFlags(fs)
	_ = fs.Parse(args)
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })

	if *apply {
		var cpus, swap int
		detectHost(fs, r.ram, &cpus, &swap)
	}
	st := openStore(*db)
	defer st.Close()
	old, err := st.GetSite(*name)
	fail(err)
	cur := old
	cur.PHPIni = maps.Clone(old.PHPIni)

	onlyFor := func(flagName string, kinds ...site.Kind) {
		if given[flagName] && !slices.Contains(kinds, cur.Kind) {
			names := make([]string, len(kinds))
			for i, k := range kinds {
				names[i] = string(k)
			}
			fail(fmt.Errorf("-%s applies to %s sites only, and site %q is a %s site", flagName, strings.Join(names, " and "), cur.Name, cur.Kind))
		}
	}
	for _, f := range []string{"php", "ini", "ini-reset"} {
		onlyFor(f, site.PHP)
	}
	onlyFor("entry", site.Binary)
	for _, f := range []string{"port", "mem"} {
		onlyFor(f, site.Binary, site.Mise)
	}
	onlyFor("mise-task", site.Mise)
	onlyFor("upstream", site.Proxy)

	if given["root-sub"] {
		if cur.Kind == site.Proxy {
			fail(fmt.Errorf("a proxy site has no web root"))
		}
		if !site.ValidSubdir(*rootSub) {
			fail(fmt.Errorf("-root-sub %q: use folder names of letters, digits, '.', '_' and '-', separated by '/', none starting with a dot", *rootSub))
		}
		cur.Root = site.RootFor(cur.User, *rootSub)
	}
	if given["php"] {
		cur.PHPVersion = *php
	}
	if *iniReset {
		cur.PHPIni = nil
	}
	for _, kv := range ini {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			fail(fmt.Errorf("-ini %q: write it as name=value", kv))
		}
		if cur.PHPIni == nil {
			cur.PHPIni = map[string]string{}
		}
		cur.PHPIni[k] = v
	}
	if given["entry"] {
		cur.Entry = *entry
	}
	if given["port"] && *port == 0 {
		fail(errors.New("binary and mise apps must listen on a TCP port"))
	} else if given["port"] {
		cur.AppPort = *port
		if f := mpnPortHolder(*port); f != "" {
			fail(fmt.Errorf("port %d is used by the panel (%s); an app needs another port", *port, f))
		}
		if other, err := portHolder(st, cur.Name, *port); err != nil {
			fail(err)
		} else if other != "" {
			fail(fmt.Errorf("port %d is already used by site %q", *port, other))
		}
	}
	if given["mem"] {
		cur.MemMB = *mem
	}
	if given["mise-task"] {
		cur.MiseTask = *miseTask
		if cur.MiseTask != "" {
			tasks, err := listMiseTasks(cur)
			fail(err)
			found := false
			for _, task := range tasks {
				if task.Name == cur.MiseTask {
					found = true
					break
				}
			}
			if !found {
				fail(fmt.Errorf("mise task %q is not present in this site's mise.toml", cur.MiseTask))
			}
		}
	}
	if given["upstream"] {
		cur.Upstream = *upstream
	}
	fail(cur.Validate())
	if cur.Kind == site.PHP && cur.PHPVersion != old.PHPVersion && !slices.Contains(site.InstalledPHP(), cur.PHPVersion) {
		fail(fmt.Errorf("php %s is not installed on this machine; install it first with: mpn plan -php %s -apply", cur.PHPVersion, cur.PHPVersion))
	}

	plan, err := provision.ChangePlan(old, cur, provision.Options{
		Profile: tune.New(tune.Input{RAMMB: *r.ram, CPUs: 1}),
		TLS:     *r.tls, CertDir: *r.certDir, AppMemMB: *r.appMem,
	})
	fail(err)
	if !*apply {
		fmt.Print(plan)
		fmt.Println("# dry run: nothing was changed. With -apply the files are made and then the settings are stored.")
		return
	}
	runApply(plan)
	fail(st.UpdateSite(cur))
	fmt.Printf("settings of site %q stored\n", cur.Name)
}

// portHolder returns the name of another site that uses the port, or "".
func portHolder(st interface {
	ListSites() ([]site.Site, error)
}, self string, port int) (string, error) {
	all, err := st.ListSites()
	if err != nil {
		return "", err
	}
	for _, s := range all {
		if s.Name != self && (s.Kind == site.Binary || s.Kind == site.Mise) && s.AppPort == port {
			return s.Name, nil
		}
	}
	return "", nil
}
