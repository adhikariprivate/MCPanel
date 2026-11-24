package webauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"
)

var delegatedUserRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{2,31}$`)
var delegatedSiteRE = regexp.MustCompile(`^[a-f0-9]{32}$`)

// DelegatedAccess is the panel-owned access-control sidecar. It lives beside account.json, where the unprivileged
// panel process can update it without granting site users access to the root-helper database.
type DelegatedAccess struct {
	Version int                      `json:"version"`
	Users   map[string]DelegatedUser `json:"users"`
}

type DelegatedUser struct {
	Hash    string                       `json:"hash"`
	Enabled bool                         `json:"enabled"`
	Sites   map[string]map[string]string `json:"sites"` // stable site ID -> capability -> allow|deny
	Updated int64                        `json:"updated"`
}

func NewDelegatedAccess() DelegatedAccess {
	return DelegatedAccess{Version: 1, Users: map[string]DelegatedUser{}}
}

func LoadDelegatedAccess(path string) (DelegatedAccess, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return NewDelegatedAccess(), nil
	}
	if err != nil {
		return DelegatedAccess{}, err
	}
	var a DelegatedAccess
	if err := json.Unmarshal(b, &a); err != nil {
		return DelegatedAccess{}, fmt.Errorf("%s: %w", path, err)
	}
	if a.Version != 1 || a.Users == nil {
		return DelegatedAccess{}, fmt.Errorf("%s: unsupported or incomplete delegated access data", path)
	}
	for name, u := range a.Users {
		if !delegatedUserRE.MatchString(name) || u.Hash == "" || u.Sites == nil {
			return DelegatedAccess{}, fmt.Errorf("%s: invalid delegated user record", path)
		}
		for site, grants := range u.Sites {
			if !validAccessSite(site) {
				return DelegatedAccess{}, fmt.Errorf("%s: invalid site assignment", path)
			}
			for cap, effect := range grants {
				if !slices.Contains(AccessCapabilities, cap) || (effect != "allow" && effect != "deny") {
					return DelegatedAccess{}, fmt.Errorf("%s: invalid permission grant", path)
				}
			}
		}
	}
	return a, nil
}

func SaveDelegatedAccess(path string, a DelegatedAccess) error {
	if a.Version != 1 || a.Users == nil {
		return errors.New("invalid delegated access data")
	}
	for name, u := range a.Users {
		if !delegatedUserRE.MatchString(name) || u.Hash == "" || u.Sites == nil {
			return errors.New("invalid delegated user")
		}
		for site, grants := range u.Sites {
			if !validAccessSite(site) {
				return errors.New("invalid site assignment")
			}
			for cap, effect := range grants {
				if !slices.Contains(AccessCapabilities, cap) || (effect != "allow" && effect != "deny") {
					return errors.New("invalid permission grant")
				}
			}
		}
	}
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".delegated-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil && !isWindows() {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (a DelegatedAccess) Authenticate(user, password string) bool {
	u, ok := a.Users[user]
	return ok && u.Enabled && Verify(password, u.Hash)
}

func (a DelegatedAccess) CheckLimited(user, password string) (bool, error) {
	u, ok := a.Users[user]
	good, err := (Account{User: user, Hash: u.Hash}).CheckLimited(user, password)
	return ok && u.Enabled && good, err
}

func (a DelegatedAccess) Allows(user, site, capability string) bool {
	if !slices.Contains(AccessCapabilities, capability) {
		return false
	}
	u, ok := a.Users[user]
	if !ok || !u.Enabled {
		return false
	}
	grants, ok := u.Sites[site]
	if !ok {
		return false
	}
	return grants[capability] == "allow" // explicit deny and inherited/unset both fail closed
}

func (a DelegatedAccess) AssignedSites(user string) []string {
	u, ok := a.Users[user]
	if !ok || !u.Enabled {
		return nil
	}
	out := make([]string, 0, len(u.Sites))
	for site := range u.Sites {
		out = append(out, site)
	}
	slices.Sort(out)
	return out
}

func validAccessSite(site string) bool {
	return delegatedSiteRE.MatchString(site)
}

func NewDelegatedUser(hash string, sites map[string]map[string]string) DelegatedUser {
	return DelegatedUser{Hash: hash, Enabled: true, Sites: sites, Updated: time.Now().Unix()}
}

var AccessCapabilities = []string{
	"site.view", "site.settings", "files.read", "files.write", "files.delete", "files.permissions",
	"ssl.view", "ssl.manage", "cron.view", "cron.manage", "logs.view", "activity.view", "database.view",
	"backup.view", "email.view", "email.manage",
	"terminal.open", "ssh.manage", "wordpress.view", "wordpress.manage", "cloud.view", "cloud.manage", "vhost.view", "vhost.manage", "app.manage",
}
