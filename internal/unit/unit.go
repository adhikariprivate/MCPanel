// Package unit renders systemd service units for supervised site apps.
package unit

import (
	"fmt"
	"strings"

	"mpn/internal/site"
)

// Name is the unit file name for a site.
func Name(s site.Site) string { return "app-" + s.Name + ".service" }

// Binary renders a unit for a compiled web application installed under its site's home, outside the public document root.
func Binary(s site.Site, memoryMB int) (string, error) {
	if s.Kind != site.Binary {
		return "", fmt.Errorf("site %q is %s, not binary", s.Name, s.Kind)
	}
	if err := s.Validate(); err != nil {
		return "", err
	}
	lines := []string{
		"[Unit]",
		"Description=Binary app " + s.Name,
		"After=network.target",
		"",
		"[Service]",
		"User=" + s.User,
		"WorkingDirectory=" + s.Root,
		fmt.Sprintf("Environment=PORT=%d", s.AppPort),
		"ExecCondition=/usr/bin/test -x /home/" + s.User + "/" + s.Entry,
		"ExecStart=/home/" + s.User + "/" + s.Entry,
		"Restart=always",
		"RestartSec=3",
		"NoNewPrivileges=yes",
		"PrivateTmp=yes",
	}
	if memoryMB > 0 {
		lines = append(lines, fmt.Sprintf("MemoryMax=%dM", memoryMB))
	}
	lines = append(lines, "", "[Install]", "WantedBy=multi-user.target", "")
	return strings.Join(lines, "\n"), nil
}

// Mise renders a supervised mise task. Runtime installation and task execution both happen as the site account.
func Mise(s site.Site, memoryMB int) (string, error) {
	if s.Kind != site.Mise {
		return "", fmt.Errorf("site %q is %s, not mise", s.Name, s.Kind)
	}
	if err := s.Validate(); err != nil {
		return "", err
	}
	if s.MiseTask == "" {
		return "", fmt.Errorf("site %q has no mise task selected", s.Name)
	}
	home := "/home/" + s.User
	lines := []string{
		"[Unit]", "Description=Mise app " + s.Name, "After=network.target", "",
		"[Service]", "User=" + s.User, "WorkingDirectory=" + s.Root,
		fmt.Sprintf("Environment=PORT=%d", s.AppPort),
		"Environment=HOME=" + home,
		"Environment=MISE_DATA_DIR=" + site.MiseDataDir(s.Name),
		"Environment=MISE_INSTALLS_DIR=" + site.MiseInstallsDir(s.Name),
		"Environment=MISE_CACHE_DIR=" + home + "/.cache/mise",
		"Environment=MISE_CONFIG_DIR=" + home + "/.config/mise",
		"Environment=MISE_TRUSTED_CONFIG_PATHS=" + s.Root,
		"ExecStartPre=+/usr/local/bin/mpn site mise-install -name " + s.Name,
		"ExecStart=/usr/local/bin/mise run " + s.MiseTask,
		"Restart=on-failure", "RestartSec=3", "NoNewPrivileges=yes", "PrivateTmp=yes",
	}
	if memoryMB > 0 {
		lines = append(lines, fmt.Sprintf("MemoryMax=%dM", memoryMB))
	}
	lines = append(lines, "", "[Install]", "WantedBy=multi-user.target", "")
	return strings.Join(lines, "\n"), nil
}
