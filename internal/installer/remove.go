package installer

import (
	"fmt"
	"slices"
	"strings"

	"mpn/internal/distro"
	"mpn/internal/site"
)

// The removal of what the Services page installs. Only what can go without losing anything of the person: a PHP version
// that no website runs on, the database browser and fail2ban. The packages are removed (apt remove, dnf remove), not purged, so the
// settings of PHP stay in /etc and come back with a new install. The database servers and the mail programs are never removed here:
// that would take every database or mailbox with them.

// PHPRemovePlan removes one PHP version. sites are the websites of the machine; none may run on v, and the database browser, which
// runs on PHP 8.3, must be gone before that version is.
func PHPRemovePlan(v string, sites []site.Site, pmaInstalled bool) (Plan, error) {
	if !slices.Contains(site.PHPVersions, v) {
		return nil, fmt.Errorf("php version %q is not offered", v)
	}
	if users := usersOf(sites, func(s site.Site) bool { return s.Kind == site.PHP && s.PHPVersion == v }); len(users) > 0 {
		return nil, fmt.Errorf("PHP %s runs the websites %s; give them another version first", v, strings.Join(users, ", "))
	}
	if v == pmaPHP && pmaInstalled {
		return nil, fmt.Errorf("phpMyAdmin runs on PHP %s; remove phpMyAdmin first", v)
	}
	svc := distro.PHPService(v)
	acts := []Action{Run("sh", "-c", "systemctl disable --now "+svc+" 2>/dev/null || true")}
	if distro.OwnPHP(v) {
		// EL, PHP 7.0 to 7.3: a tarball of mpn-packages in /opt/mpn/php/<v> and a unit of mpn.
		acts = append(acts,
			Run("rm", "-f", "/etc/systemd/system/"+svc+".service"),
			Run("rm", "-rf", "/opt/mpn/php/"+v),
			Run("systemctl", "daemon-reload"))
	} else {
		pkgs := []string{phpPackagePattern(v)}
		acts = append(acts, Run(distro.UninstallCommand(pkgs...)...))
	}
	return Plan{{Name: "remove PHP " + v, Actions: acts}}, nil
}

// phpPackagePattern names every package of one version for the package manager: php8.3-* on Debian, php83-* on EL (Remi).
func phpPackagePattern(v string) string {
	if distro.IsEL() {
		return "php" + strings.ReplaceAll(v, ".", "") + "-*"
	}
	return "php" + v + "-*"
}

// PMARemovePlan removes the database browser: its files, its pool, its address in nginx, its sweep and the flag of the panel. Its
// temporary database accounts are dropped first by the sweep, run once more with every account counted as expired. The PHP 8.3 it ran
// on stays; it is removed from the Services page like any other version when nothing uses it.
func PMARemovePlan(panelPort int) Plan {
	unit := "/etc/systemd/system/mpn-web.service"
	return Plan{
		{Name: "remove phpMyAdmin", Actions: []Action{
			Run("sh", "-c", mpnBinary+" db pmaclean -all -apply || true"),
			Run("sh", "-c", "systemctl disable --now mpn-pma-clean.timer 2>/dev/null || true"),
			Run("rm", "-f", "/etc/systemd/system/mpn-pma-clean.service", "/etc/systemd/system/mpn-pma-clean.timer"),
			Run("rm", "-f", "/etc/nginx/conf.d/02-mpn-pma.conf", distro.PHPPoolDir(pmaPHP)+"mpn-pma.conf"),
			Run("nginx", "-t"),
			ReloadNginx(),
			Run("sh", "-c", stopUnusedFPM(pmaPHP)+" || true"),
			Run("sh", "-c", "systemctl try-restart "+distro.PHPService(pmaPHP)+" 2>/dev/null || true"),
			Run("rm", "-rf", pmaDir, pmaConf, pmaState),
			// The account of the browser is removed only when mpn made it (its mark), like the accounts of websites.
			Run("sh", "-c", `if getent passwd `+pmaAccount+` | cut -d: -f5 | grep -qx '`+pmaMark+`'; then userdel `+pmaAccount+`; fi; getent group `+pmaAccount+` >/dev/null && groupdel `+pmaAccount+` 2>/dev/null || true`),
			Run("sh", "-c", `sed -i 's| -pma-port [0-9]*||' `+unit),
			Run("systemctl", "daemon-reload"),
			Run("systemd-run", "--quiet", "--collect", "--on-active=4", "--unit=mpn-web-restart", "systemctl", "restart", "mpn-web"),
		}},
	}
}

// Fail2banRemovePlan stops fail2ban and removes its package; the jails mpn wrote stay in /etc/fail2ban/jail.d for a new install.
func Fail2banRemovePlan() Plan {
	return Plan{{Name: "remove fail2ban", Actions: []Action{
		Run("sh", "-c", "systemctl disable --now fail2ban 2>/dev/null || true"),
		// EL names the program fail2ban-server (with fail2ban-systemd and fail2ban-selinux beside it); dnf removing "fail2ban" alone
		// succeeds and removes nothing (seen on AlmaLinux 9.8).
		Run(distro.UninstallCommand(fail2banPackages()...)...),
	}}}
}

func usersOf(sites []site.Site, uses func(site.Site) bool) []string {
	var out []string
	for _, s := range sites {
		if uses(s) {
			out = append(out, s.Name)
		}
	}
	return out
}

func fail2banPackages() []string {
	if distro.IsEL() {
		return []string{"fail2ban-server", "fail2ban-systemd", "fail2ban-selinux"}
	}
	return []string{"fail2ban"}
}
