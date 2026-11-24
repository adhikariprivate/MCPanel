// Package bgservice validates and renders per-site systemd background services.
package bgservice

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"mpn/internal/distro"
	"mpn/internal/site"
)

type Kind string

const (
	PHP    Kind = "php"
	Binary Kind = "binary"
	Mise   Kind = "mise"
)

type Service struct {
	ID          int64  `json:"id"`
	Site        string `json:"site"`
	Name        string `json:"name"`
	Kind        Kind   `json:"kind"`
	Command     string `json:"command,omitempty"`
	Task        string `json:"task,omitempty"`
	MemoryMB    int    `json:"memory_mb,omitempty"`
	Status      string `json:"status,omitempty"`
	StatusError string `json:"status_error,omitempty"`
}

var nameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9 _.-]{0,47}$`)

func ValidName(s string) error {
	if !nameRE.MatchString(s) || strings.TrimSpace(s) != s {
		return fmt.Errorf("use a 1–48 character service name")
	}
	return nil
}

// Args parses a conservative quoted-argument syntax. Shell syntax and expansions are rejected.
func Args(s string) ([]string, error) {
	if len(s) == 0 || len(s) > 2048 {
		return nil, fmt.Errorf("command must be between 1 and 2048 characters")
	}
	var out []string
	var b strings.Builder
	var quote rune
	escaped, have := false, false
	for _, r := range s {
		if r == '\n' || r == '\r' || r == 0 || strings.ContainsRune(";&|<>$`(){}", r) {
			return nil, fmt.Errorf("shell operators, expansions, and newlines are not allowed")
		}
		if escaped {
			b.WriteRune(r)
			escaped = false
			have = true
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
				have = true
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			have = true
			continue
		}
		if r == ' ' || r == '\t' {
			if have {
				out = append(out, b.String())
				b.Reset()
				have = false
			}
			continue
		}
		b.WriteRune(r)
		have = true
	}
	if escaped || quote != 0 {
		return nil, fmt.Errorf("command has an unfinished quote or escape")
	}
	if have {
		out = append(out, b.String())
	}
	if len(out) == 0 || len(out) > 64 {
		return nil, fmt.Errorf("command must contain between 1 and 64 arguments")
	}
	for _, a := range out {
		if a == "" || len(a) > 512 {
			return nil, fmt.Errorf("arguments must be between 1 and 512 characters")
		}
	}
	return out, nil
}

func Valid(s Service) error {
	if err := ValidName(s.Name); err != nil {
		return err
	}
	if s.MemoryMB != 0 && (s.MemoryMB < 32 || s.MemoryMB > 65536) {
		return fmt.Errorf("memory limit must be 0 or between 32 and 65536 MB")
	}
	switch s.Kind {
	case PHP:
		if s.Task != "" {
			return fmt.Errorf("PHP commands do not use a mise task")
		}
		if _, err := Args(s.Command); err != nil {
			return err
		}
		return nil
	case Binary:
		if s.Task != "" {
			return fmt.Errorf("binary services do not use a mise task")
		}
		if _, err := Args(s.Command); err != nil {
			return err
		}
		parts, _ := Args(s.Command)
		p := parts[0]
		if strings.HasPrefix(p, "/") || filepath.Clean(p) != p || p == "." || p == ".." || strings.HasPrefix(p, "../") {
			return fmt.Errorf("binary path must be relative to the site home")
		}
		return nil
	case Mise:
		if s.Command != "" {
			return fmt.Errorf("mise services select a task instead of a command")
		}
		if !site.ValidMiseTask(s.Task) {
			return fmt.Errorf("invalid mise task name")
		}
		return nil
	default:
		return fmt.Errorf("service type must be php, binary, or mise")
	}
}

func UnitName(siteName string, id int64) string {
	return fmt.Sprintf("app-%s-bg-%d.service", siteName, id)
}

// SystemdEscape quotes one argv item using systemd's double-quoted ExecStart grammar.
func SystemdEscape(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	v = strings.ReplaceAll(v, `%`, `%%`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	v = strings.ReplaceAll(v, "\t", `\t`)
	return `"` + v + `"`
}

func Render(siteInfo site.Site, s Service) (string, error) {
	if err := siteInfo.Validate(); err != nil {
		return "", fmt.Errorf("invalid site: %w", err)
	}
	if err := Valid(s); err != nil {
		return "", err
	}
	if s.ID < 1 {
		return "", fmt.Errorf("service needs a saved ID")
	}
	var exe string
	var args []string
	switch s.Kind {
	case PHP:
		if siteInfo.Kind != site.PHP {
			return "", fmt.Errorf("PHP commands require a PHP site")
		}
		a, err := Args(s.Command)
		if err != nil {
			return "", err
		}
		if !site.ValidPHPVersion(siteInfo.PHPVersion) {
			return "", fmt.Errorf("site PHP version is not supported")
		}
		exe, args = distro.PHPCLIBinary(siteInfo.PHPVersion), a
	case Binary:
		a, err := Args(s.Command)
		if err != nil {
			return "", err
		}
		exe, args = "/home/"+siteInfo.User+"/"+a[0], a[1:]
	case Mise:
		if siteInfo.Kind != site.Mise {
			return "", fmt.Errorf("mise tasks require a mise site")
		}
		exe, args = "/usr/local/bin/mise", []string{"run", s.Task}
	default:
		return "", fmt.Errorf("unsupported service type")
	}
	unit := []string{"[Unit]", "Description=MCPN background service " + s.Name + " (site " + siteInfo.Name + ")", "After=network.target", "", "[Service]", "Type=simple", "User=" + siteInfo.User, "WorkingDirectory=" + siteInfo.Root, "NoNewPrivileges=yes", "PrivateTmp=yes", "Restart=on-failure", "RestartSec=3", "StandardOutput=journal", "StandardError=journal"}
	if s.Kind == Binary {
		args, _ := Args(s.Command)
		path := "/home/" + siteInfo.User + "/" + args[0]
		unit = append(unit, "ExecCondition=/usr/bin/test -x "+SystemdEscape(path))
	}
	if s.Kind == Mise {
		home := "/home/" + siteInfo.User
		unit = append(unit, "Environment=HOME="+home, "Environment=MISE_DATA_DIR="+site.MiseDataDir(siteInfo.Name), "Environment=MISE_INSTALLS_DIR="+site.MiseInstallsDir(siteInfo.Name), "Environment=MISE_CACHE_DIR="+home+"/.cache/mise", "Environment=MISE_CONFIG_DIR="+home+"/.config/mise", "Environment=MISE_TRUSTED_CONFIG_PATHS="+siteInfo.Root)
		unit = append(unit, "ExecStartPre=+/usr/local/bin/mpn site mise-install -name "+siteInfo.Name)
	}
	cmd := []string{SystemdEscape(exe)}
	for _, a := range args {
		cmd = append(cmd, SystemdEscape(a))
	}
	unit = append(unit, "ExecStart="+strings.Join(cmd, " "))
	if s.MemoryMB > 0 {
		unit = append(unit, fmt.Sprintf("MemoryMax=%dM", s.MemoryMB))
	}
	unit = append(unit, "", "[Install]", "WantedBy=multi-user.target", "")
	return strings.Join(unit, "\n"), nil
}
