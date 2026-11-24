package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"mpn/internal/bgservice"
	"mpn/internal/installer"
	"mpn/internal/site"
)

func runSiteBackground(args []string) {
	if len(args) == 0 {
		fail(errors.New("usage: mpn site background <list|add|edit|start|stop|restart|logs|remove> -name SITE"))
	}
	sub := args[0]
	fs := flag.NewFlagSet("site background "+sub, flag.ExitOnError)
	db := fs.String("db", defaultDB, "database path")
	name := fs.String("name", "", "site name")
	id := fs.Int64("id", 0, "background service ID")
	stdin := fs.Bool("stdin", false, "read service fields as JSON from standard input")
	apply := fs.Bool("apply", false, "apply changes on this machine")
	_ = fs.Parse(args[1:])
	if !site.ValidName(*name) {
		fail(fmt.Errorf("invalid site name %q", *name))
	}
	if sub != "list" && sub != "logs" && !*apply {
		fail(errors.New("-apply is required to change a background service"))
	}
	st := openStore(*db)
	defer st.Close()
	s, err := st.GetSite(*name)
	fail(err)
	services, err := st.BackgroundServices(s.Name)
	fail(err)
	if sub == "list" {
		for i := range services {
			services[i].Status, err = backgroundUnitStatus(bgservice.UnitName(s.Name, services[i].ID))
			if err != nil {
				services[i].StatusError = err.Error()
			}
		}
		printJSON(services)
		return
	}
	if sub == "add" {
		requireBackgroundInput(*stdin)
		var v bgservice.Service
		readBackground(&v)
		if err := validateBackgroundForSite(s, v); err != nil {
			fail(err)
		}
		newID, err := st.AddBackgroundService(s.Name, v)
		fail(err)
		v.ID = newID
		content, err := bgservice.Render(s, v)
		if err != nil {
			_ = st.DeleteBackgroundService(s.Name, newID)
			fail(err)
		}
		if err := installBackgroundUnit(s, v, content, false); err != nil {
			unitName := bgservice.UnitName(s.Name, newID)
			_ = runPlanQuiet(installer.Plan{{Name: "remove incomplete background service", Actions: []installer.Action{installer.Run("rm", "-f", "/etc/systemd/system/"+unitName), installer.Run("systemctl", "daemon-reload")}}})
			_ = st.DeleteBackgroundService(s.Name, newID)
			fail(err)
		}
		fmt.Printf("background service %d saved stopped\n", newID)
		return
	}
	v, err := st.BackgroundService(s.Name, *id)
	fail(err)
	unitName := bgservice.UnitName(s.Name, v.ID)
	switch sub {
	case "start":
		if err := validateRuntime(s, v); err != nil {
			fail(err)
		}
		backgroundRun(installer.Run("systemctl", "enable", "--now", unitName))
		fmt.Printf("background service %d started\n", v.ID)
	case "stop":
		backgroundRun(installer.Run("systemctl", "disable", "--now", unitName))
		fmt.Printf("background service %d stopped\n", v.ID)
	case "restart":
		if err := validateRuntime(s, v); err != nil {
			fail(err)
		}
		backgroundRun(installer.Run("systemctl", "restart", unitName))
		fmt.Printf("background service %d restarted\n", v.ID)
	case "logs":
		out, err := exec.Command("journalctl", "-u", unitName, "-n", "100", "--no-pager", "-o", "short-iso", "-q").CombinedOutput()
		if err != nil {
			fail(fmt.Errorf("read logs: %w: %s", err, strings.TrimSpace(string(out))))
		}
		fmt.Print(string(out))
	case "remove", "del":
		actions, err := backgroundCleanupActions(s.Name, s.User, v)
		fail(err)
		if len(actions) > 0 {
			backgroundRun(actions...)
		}
		fail(st.DeleteBackgroundService(s.Name, v.ID))
		fmt.Printf("background service %d removed\n", v.ID)
	case "edit":
		requireBackgroundInput(*stdin)
		var next bgservice.Service
		readBackground(&next)
		if err := validateBackgroundForSite(s, next); err != nil {
			fail(err)
		}
		next.ID = v.ID
		wasActiveState, _ := backgroundUnitStatus(unitName)
		wasActive := wasActiveState == "active"
		oldContent, err := bgservice.Render(s, v)
		fail(err)
		newContent, err := bgservice.Render(s, next)
		fail(err)
		if err := st.UpdateBackgroundService(s.Name, v.ID, next); err != nil {
			fail(err)
		}
		if err := installBackgroundUnit(s, v, newContent, true); err != nil {
			_ = st.UpdateBackgroundService(s.Name, v.ID, v)
			if rollbackErr := installBackgroundUnit(s, v, oldContent, true); rollbackErr != nil {
				fail(fmt.Errorf("could not install replacement or restore the previous unit: %v; restore error: %w", err, rollbackErr))
			}
			fail(err)
		}
		if wasActive {
			if err := exec.Command("systemctl", "restart", unitName).Run(); err != nil {
				_ = st.UpdateBackgroundService(s.Name, v.ID, v)
				_ = installBackgroundUnit(s, v, oldContent, true)
				_ = exec.Command("systemctl", "restart", unitName).Run()
				fail(fmt.Errorf("replacement service failed; previous definition restored: %w", err))
			}
		}
		fmt.Printf("background service %d updated\n", v.ID)
	default:
		fail(errors.New("unknown background service operation"))
	}
}

func requireBackgroundInput(ok bool) {
	if !ok {
		fail(errors.New("this operation needs -stdin"))
	}
}
func readBackground(v *bgservice.Service) {
	b, err := io.ReadAll(io.LimitReader(os.Stdin, 16<<10))
	fail(err)
	if err := json.Unmarshal(b, v); err != nil {
		fail(fmt.Errorf("invalid service JSON: %w", err))
	}
}
func validateBackgroundForSite(s site.Site, v bgservice.Service) error {
	if err := bgservice.Valid(v); err != nil {
		return err
	}
	if v.Kind == bgservice.PHP && s.Kind != site.PHP {
		return fmt.Errorf("PHP commands require a PHP site")
	}
	if v.Kind == bgservice.Mise && s.Kind != site.Mise {
		return fmt.Errorf("mise tasks require a mise site")
	}
	if v.Kind == bgservice.Mise {
		tasks, err := listMiseTasks(s)
		if err != nil {
			return err
		}
		found := false
		for _, t := range tasks {
			if t.Name == v.Task {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("mise task %q is not present in this site's mise.toml", v.Task)
		}
	}
	return nil
}
func validateRuntime(s site.Site, v bgservice.Service) error {
	if err := validateBackgroundForSite(s, v); err != nil {
		return err
	}
	if v.Kind == bgservice.Binary {
		parts, err := bgservice.Args(v.Command)
		if err != nil {
			return err
		}
		p := filepath.Join("/home/"+s.User, parts[0])
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			return fmt.Errorf("cannot resolve executable: %w", err)
		}
		home, err := filepath.EvalSymlinks("/home/" + s.User)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(home, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
			return fmt.Errorf("binary must resolve inside the site's home")
		}
		fi, err := os.Stat(resolved)
		if err != nil || fi.IsDir() || fi.Mode()&0111 == 0 {
			return fmt.Errorf("binary path must resolve to an executable file")
		}
	}
	return nil
}
func backgroundUnitStatus(unit string) (string, error) {
	b, err := exec.Command("systemctl", "show", "--property=ActiveState", "--value", unit).CombinedOutput()
	if err != nil {
		return "unknown", fmt.Errorf("cannot inspect systemd unit %s: %s", unit, strings.TrimSpace(string(b)))
	}
	state := strings.TrimSpace(string(b))
	switch state {
	case "active", "inactive", "failed":
		return state, nil
	default:
		return "unknown", fmt.Errorf("systemd reported an unexpected state %q for %s", state, unit)
	}
}
func installBackgroundUnit(siteInfo site.Site, service bgservice.Service, content string, replace bool) error {
	unit := bgservice.UnitName(siteInfo.Name, service.ID)
	path := "/etc/systemd/system/" + unit
	if replace {
		if err := requireOwnedBackgroundUnit(siteInfo.Name, siteInfo.User, service); err != nil {
			return err
		}
	} else if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("refusing to overwrite existing unit %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return runPlanQuiet(installer.Plan{{Name: "background service unit", Actions: []installer.Action{installer.WriteFile("/etc/systemd/system/"+unit, content, 0o644), installer.Run("systemctl", "daemon-reload")}}})
}

func backgroundUnitPresent(siteName, user string, service bgservice.Service) (bool, error) {
	path := "/etc/systemd/system/" + bgservice.UnitName(siteName, service.ID)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		// A unit file can be removed while systemd still has its old definition
		// loaded and its process running. Treat that as present only when systemd
		// identifies the exact generated fragment path; otherwise fail closed.
		out, showErr := exec.Command("systemctl", "show", "--property=LoadState,FragmentPath", bgservice.UnitName(siteName, service.ID)).CombinedOutput()
		if showErr != nil {
			return false, fmt.Errorf("cannot inspect missing background unit %s: %w: %s", bgservice.UnitName(siteName, service.ID), showErr, strings.TrimSpace(string(out)))
		}
		var loadState, fragmentPath string
		for _, line := range strings.Split(string(out), "\n") {
			key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
			if !ok {
				continue
			}
			switch key {
			case "LoadState":
				loadState = value
			case "FragmentPath":
				fragmentPath = value
			}
		}
		if loadState == "not-found" || loadState == "" {
			return false, nil
		}
		if fragmentPath != path {
			return false, fmt.Errorf("refusing loaded background unit %s from unexpected fragment %q", bgservice.UnitName(siteName, service.ID), fragmentPath)
		}
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, fmt.Errorf("refusing non-regular background unit %s", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if !strings.Contains(string(b), "Description=MCPN background service ") || !strings.Contains(string(b), "(site "+siteName+")\n") || !strings.Contains(string(b), "User="+user+"\n") {
		return false, fmt.Errorf("unit %s is not identifiable as an MCPN background service", path)
	}
	return true, nil
}

func requireOwnedBackgroundUnit(siteName, user string, service bgservice.Service) error {
	_, err := backgroundUnitPresent(siteName, user, service)
	return err
}

func backgroundCleanupActions(siteName, user string, service bgservice.Service) ([]installer.Action, error) {
	unit := bgservice.UnitName(siteName, service.ID)
	path := "/etc/systemd/system/" + unit
	present, err := backgroundUnitPresent(siteName, user, service)
	if err != nil || !present {
		return nil, err
	}
	if _, err := os.Lstat(path); err == nil {
		return []installer.Action{
			installer.Run("systemctl", "disable", "--now", unit),
			installer.Run("rm", "-f", path),
			installer.Run("systemctl", "daemon-reload"),
		}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	// The fragment vanished while systemd still had it loaded. Stop first,
	// then remove only enablement symlinks that resolve to this exact fragment.
	// `disable --now` can fail on the missing fragment before it stops the unit.
	actions := []installer.Action{installer.Run("systemctl", "stop", unit)}
	root := "/etc/systemd/system"
	err = filepath.WalkDir(root, func(link string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(filepath.Base(filepath.Dir(link)), ".wants") || filepath.Base(link) != unit {
			return nil
		}
		info, err := os.Lstat(link)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return nil
		}
		target, err := os.Readlink(link)
		if err != nil {
			return err
		}
		if backgroundLinkTarget(link, target) == path {
			actions = append(actions, installer.Run("rm", "-f", link))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("inspect enablement links for %s: %w", unit, err)
	}
	actions = append(actions, installer.Run("systemctl", "daemon-reload"))
	return actions, nil
}

func backgroundLinkTarget(link, target string) string {
	if filepath.IsAbs(target) {
		return filepath.Clean(target)
	}
	return filepath.Clean(filepath.Join(filepath.Dir(link), target))
}

func backgroundRun(actions ...installer.Action) {
	runApply(installer.Plan{{Name: "background service", Actions: actions}})
}
