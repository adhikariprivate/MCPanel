// Package unit renders systemd service units for supervised site apps.
package unit

import (
	"fmt"
	"strings"

	"mpn/internal/distro"
	"mpn/internal/site"
)

// NodeRoot is where shared Node.js builds live. Each major version is expected at NodeRoot/<major>/bin/node,
// for example a symlink /opt/node/22 pointing at the installed 22.x release.
const NodeRoot = "/opt/node"

// Name is the unit file name for a site.
func Name(s site.Site) string { return "app-" + s.Name + ".service" }

// Python renders the unit for a python site. The app runs with the interpreter of the site's own virtual environment
// (site.VenvDir), so its packages are never mixed with the system's. memoryMB <= 0 means no memory cap.
func Python(s site.Site, memoryMB int) (string, error) {
	if s.Kind != site.Python {
		return "", fmt.Errorf("site %q is %s, not python", s.Name, s.Kind)
	}
	if err := s.Validate(); err != nil {
		return "", err
	}
	lines := []string{
		"[Unit]",
		"Description=Python app " + s.Name,
		"After=network.target",
		"",
		"[Service]",
		"User=" + s.User,
		"WorkingDirectory=" + s.Root,
		"Environment=PYTHONUNBUFFERED=1",
	}
	lines = append(lines, listen(s)...)
	lines = append(lines,
		fmt.Sprintf("ExecStart=%s/bin/python %s", site.VenvDir(s.User), s.Entry),
		"Restart=always",
		"RestartSec=3",
		"NoNewPrivileges=yes",
		"PrivateTmp=yes",
	)
	if memoryMB > 0 {
		lines = append(lines, fmt.Sprintf("MemoryMax=%dM", memoryMB))
	}
	lines = append(lines, "", "[Install]", "WantedBy=multi-user.target", "")
	return strings.Join(lines, "\n"), nil
}

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
		fmt.Sprintf("Environment=PORT=%d", s.NodePort),
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
		fmt.Sprintf("Environment=PORT=%d", s.NodePort),
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

// listen says where the app listens. With a port, PORT is the number. With a socket, SOCKET is its path, and PORT is the same path,
// because Node's server.listen(process.env.PORT) takes a path as it takes a number, so an app written for PORT keeps working. The
// folder is made by root before the app starts ("+" runs the line with full privileges): owned by the site's account, with the
// group of nginx and the setgid bit, so the socket the app makes belongs to that group; an old socket is removed, or the app could
// not bind. UMask 0007 gives the socket rw for its owner and group and nothing for the others.
func listen(s site.Site) []string {
	if !s.UsesSocket() {
		return []string{fmt.Sprintf("Environment=PORT=%d", s.NodePort)}
	}
	dir, sock := site.AppSocketDir(s.Name), site.AppSocket(s.Name)
	return []string{
		"Environment=SOCKET=" + sock,
		"Environment=PORT=" + sock,
		fmt.Sprintf("ExecStartPre=+/bin/sh -c 'install -d -m 2750 -o %s -g %s %s && rm -f %s'", s.User, distro.WebUser(), dir, sock),
		"UMask=0007",
	}
}

// Node renders the unit for a node site. memoryMB <= 0 means no memory cap.
// MemoryMax only takes effect on a unified cgroup v2 hierarchy.
func Node(s site.Site, memoryMB int) (string, error) {
	if s.Kind != site.Node {
		return "", fmt.Errorf("site %q is %s, not node", s.Name, s.Kind)
	}
	if err := s.Validate(); err != nil {
		return "", err
	}
	lines := []string{
		"[Unit]",
		"Description=Node.js app " + s.Name,
		"After=network.target",
		"",
		"[Service]",
		"User=" + s.User,
		"WorkingDirectory=" + s.Root,
		"Environment=NODE_ENV=production",
	}
	lines = append(lines, listen(s)...)
	lines = append(lines,
		fmt.Sprintf("ExecStart=%s/%s/bin/node %s", NodeRoot, s.NodeVersion, s.Entry),
		"Restart=always",
		"RestartSec=3",
		"NoNewPrivileges=yes",
		"PrivateTmp=yes",
	)
	if memoryMB > 0 {
		lines = append(lines, fmt.Sprintf("MemoryMax=%dM", memoryMB))
	}
	lines = append(lines, "", "[Install]", "WantedBy=multi-user.target", "")
	return strings.Join(lines, "\n"), nil
}
