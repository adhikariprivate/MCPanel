package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"mpn/internal/site"
)

type miseTask struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// listMiseTasks asks mise to read the site's config as the site account. It never evaluates a task and never trusts
// configuration as root. All paths and environment values are derived from the validated stored site.
func listMiseTasks(s site.Site) ([]miseTask, error) {
	if s.Kind != site.Mise {
		return nil, fmt.Errorf("site %q is not a mise site", s.Name)
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	home := "/home/" + s.User
	cmd := exec.CommandContext(ctx, "runuser", "-u", s.User, "--", "/usr/bin/env",
		"HOME="+home,
		"MISE_DATA_DIR="+site.MiseDataDir(s.Name),
		"MISE_INSTALLS_DIR="+site.MiseInstallsDir(s.Name),
		"MISE_CACHE_DIR="+home+"/.cache/mise",
		"MISE_CONFIG_DIR="+home+"/.config/mise",
		"MISE_TRUSTED_CONFIG_PATHS="+s.Root,
		"/usr/local/bin/mise", "--cd", s.Root, "tasks", "ls", "--json")
	cmd.Dir = s.Root
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("mise task discovery timed out")
		}
		if ee, ok := err.(*exec.ExitError); ok {
			message := strings.TrimSpace(string(ee.Stderr))
			if message != "" {
				return nil, fmt.Errorf("mise could not read this site's tasks: %s", message)
			}
		}
		return nil, fmt.Errorf("mise could not read this site's tasks: %w", err)
	}
	var raw []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("mise returned malformed task data: %w", err)
	}
	tasks := make([]miseTask, 0, len(raw))
	for _, task := range raw {
		if !site.ValidMiseTask(task.Name) {
			continue // names that cannot safely be passed as one task argument are not selectable
		}
		tasks = append(tasks, miseTask{Name: task.Name, Description: task.Description})
	}
	return tasks, nil
}
