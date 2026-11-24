package installer

import (
	"fmt"
	"runtime"
	"strings"

	"mpn/internal/distro"
	"mpn/internal/osinfo"
)

// One nginx version on every system. A dynamic module only loads into the exact nginx it was built for, and each distribution ships a
// different nginx (1.18 to 1.28), so the panel uses nginx 1.30.7 from nginx.org and loads two modules built for that version by
// github.com/adhikariprivate/mcpn-packages. Ubuntu 26.04 kept the nginx and the packaged modules of the distribution until 1.2.2, when
// nginx.org had no package for it (see osinfo.Target.NginxOrg and NginxUpgradeActions).
const (
	NginxVersion = "1.30.7"
	// nginxSigningFPR is a fingerprint in the signing key nginx.org publishes; the key is refused when it is not there.
	nginxSigningFPR = "573BFD6B3D8FBC641079A6ABABF5BD827BD9BF62"

	modulesRelease = "nginx-1.30.7-r2"
	// The repository was called mpn-nginx-modules until 2026-11-13; GitHub sends the old address to this one.
	modulesBase = "https://github.com/adhikariprivate/mcpn-packages/releases/download/" + modulesRelease

	nginxModulesEnabled = "/etc/nginx/modules-enabled"
	nginxKeyring        = "/usr/share/keyrings/nginx-archive-keyring.gpg"
)

// moduleRelease is the release that holds the PageSpeed module of a system: nginx-1.30.7-r2, for Debian, Ubuntu and the RHEL family
// (r1 has the same files without Ubuntu 26.04). The ModSecurity module of EL is in el-r2 (see elWAFActions).
func moduleRelease(t osinfo.Target) string {
	return modulesBase
}

// Module kinds.
const (
	ModPageSpeed   = "pagespeed"
	ModModSecurity = "modsecurity"
)

// moduleSums are the SHA-256 of the files of the release above, by system and CPU (see moduleKey). A file that does not match is never
// installed.
var moduleSums = map[string]map[string]string{
	ModPageSpeed: {
		"jammy":          "947f0da88e3fc5a20727ec5529ca94bb0eacacd1cfb157a8b4dcbd1357d4c373",
		"noble":          "02f0f6b998c319f41843794ff8e7f64dda1fa111b2fe559091dfb752f00e3145",
		"bookworm":       "8eace1494a82fe9b0b683f52a8c552948969fe3635185a7bd82386e727aea690",
		"trixie":         "d09e1549cb2723ee78b4baab82f7cbd0227a3b567641b25f140acde59a7be13b",
		"jammy-arm64":    "db8e9b41172c6413f6a8fde43770d502a9289513a11185a6b0509f4d1c5ff583",
		"noble-arm64":    "e60bb03e774fda558273a3f432c1163f7eb38c4550e70c18a3fd5a957e822dad",
		"bookworm-arm64": "9bafcf77be8a611571838556af33a37d9b9eaf73658733bbe6619a99852bc5c6",
		"trixie-arm64":   "e187f850df538aa46b849fe030f618cc5f5234296c17cc3a3d1b7ba5ee3fc2b6",
		"resolute":       "9d2be820aa8c012dc51763f5ddb6dbefaa5bd4eb0b96be1b81b64d23fb903090",
		"resolute-arm64": "47ec990508bfa266076cc8e4e225f3582ce9a5839767943240c66aa3e596b803",
		// EL 9 and 10 (AlmaLinux, Rocky Linux, CentOS Stream).
		"el9":        "9254fafd1201befb7959c30fbbf0a6e6d2cb3e0ade801218a80c376f0ccef9ea",
		"el9-arm64":  "dad43b4b51cab9c70f25cc455fae57484b896bc34cd233b6273f6188225c7f81",
		"el10":       "179cb0c29ee3e3437838d62b26f678b12340fb8a6331483be6f2f27454ec7509",
		"el10-arm64": "cb57db8d851f985e15ac9a41a7d0ffddecae9b39eb3557e715cee1dcc2262d95",
	},
	ModModSecurity: {
		"jammy":          "69fb7cad929e7274c60fc853642833bd11cf3cf72591d492ae6b6ebf03c4060c",
		"noble":          "0d938ed62893761ad3821227c3bdda227693b435a39a2a73ba38a4e3266d62c3",
		"bookworm":       "b31119687af29f796d8042368e984132f782b566cfd895f6dbf0df1556d54dd8",
		"trixie":         "f55d113369453d43dc44067ee95d1968c1367c42cbdbe5facd5e9f4213dd1445",
		"jammy-arm64":    "d4b81cd2deb5359b8ef633eb54c2806ad6198b327e95410a6f791ebfb0c6cb5c",
		"noble-arm64":    "46b18b8daaa062d72e6f1c04e64654d5f7f2a9f9f44e16685818a9e5189c2bc4",
		"bookworm-arm64": "7b66c0d1c9f1c00d6099fe74dd6518989df94df7ff22ef7c4ff10c606deb1279",
		"trixie-arm64":   "72f3b5bd4733c9a1647f626d9d5b86a6dc271e5e94e5d80dc5d44219ec55a624",
		"resolute":       "a123462b7560b057b2df366a336a4dd3fb5a34f754d72082a491869f1acd9ba4",
		"resolute-arm64": "d58bc4b7d2c1a28a09b041d5af8e26715accf725d036cbc7154260c803a02858",
		// The ModSecurity module of EL comes from el-r2 with the rules (see elWAFActions and elSums).
	},
}

var moduleNames = map[string]struct{ file, so, snippet string }{
	ModPageSpeed:   {"ngx_pagespeed_module", "ngx_pagespeed_module.so", "50-mod-pagespeed.conf"},
	ModModSecurity: {"ngx_http_modsecurity_module", "ngx_http_modsecurity_module.so", "50-mod-http-modsecurity.conf"},
}

// moduleKey is the key of moduleSums for a system and a CPU: the codename on x86-64, and the codename with -arm64 on arm64.
func moduleKey(codename, arch string) string {
	if arch == "arm64" {
		return codename + "-arm64"
	}
	return codename
}

// ModuleFile is the name of the release file of a module for a system and a CPU ("amd64" or "arm64", as runtime.GOARCH says):
// ngx_pagespeed_module-trixie-nginx1.30.7.so on x86-64, ngx_pagespeed_module-trixie-nginx1.30.7-arm64.so on arm64.
func ModuleFile(kind, codename, arch string) string {
	suffix := ""
	if arch == "arm64" {
		suffix = "-arm64"
	}
	return fmt.Sprintf("%s-%s-nginx%s%s.so", moduleNames[kind].file, codename, NginxVersion, suffix)
}

// fetchScript downloads a file, compares it with a checksum fixed in this program and only then puts it in place. A file that is
// already there with that checksum is left alone, so running the plan again changes nothing.
func fetchScript(url, sum, dest string) string {
	return `set -e
url='` + url + `'; want='` + sum + `'; dest='` + dest + `'
if [ -f "$dest" ] && [ "$(sha256sum "$dest" | cut -d' ' -f1)" = "$want" ]; then exit 0; fi
t=$(mktemp); trap 'rm -f "$t"' EXIT
curl -fsSL --retry 3 --proto '=https' -o "$t" "$url" || { echo "nginx-module-download: $url could not be downloaded" >&2; exit 1; }
[ "$(sha256sum "$t" | cut -d' ' -f1)" = "$want" ] || { echo "nginx-module-checksum: the file from $url does not match its checksum; not installed" >&2; exit 1; }
install -d -m 0755 "$(dirname "$dest")"
install -m 0644 -o root -g root "$t" "$dest"`
}

// moduleActions puts a module for the pinned nginx in place and makes nginx load it. The caller runs nginx -t and the reload. The file
// is the one built for the CPU of this program, which is the CPU of the machine it runs on.
func moduleActions(t osinfo.Target, kind string) ([]Action, error) {
	sum, ok := moduleSums[kind][moduleKey(t.Codename, runtime.GOARCH)]
	if !ok {
		return nil, fmt.Errorf("there is no %s module for nginx %s on %s %s (%s)", kind, NginxVersion, t.ID, t.Version, runtime.GOARCH)
	}
	n := moduleNames[kind]
	dest := distro.NginxModuleDir() + "/" + n.so
	return []Action{
		Run("sh", "-c", fetchScript(moduleRelease(t)+"/"+ModuleFile(kind, t.Codename, runtime.GOARCH), sum, dest)),
		Run("install", "-d", "-m", "0755", nginxModulesEnabled),
		WriteFile(nginxModulesEnabled+"/"+n.snippet, "# Written by mpn.\nload_module "+dest+";\n", 0o644),
	}, nil
}

// nginxOrgKeyScript fetches the signing key of nginx.org and keeps it only when it holds the fingerprint above.
const nginxOrgKeyScript = `set -eu
t=$(mktemp); trap 'rm -f "$t" ` + nginxKeyring + `.tmp' EXIT
curl -fsSL --retry 3 --proto '=https' https://nginx.org/keys/nginx_signing.key -o "$t"
gpg --batch --with-colons --show-keys "$t" | awk -F: '$1=="fpr"{print $10}' | grep -qx ` + nginxSigningFPR + ` \
  || { echo "the nginx.org signing key is not the one this installer expects" >&2; exit 1; }
gpg --batch --dearmor < "$t" > ` + nginxKeyring + `.tmp
mv ` + nginxKeyring + `.tmp ` + nginxKeyring

// nginxOrgSources are the apt files that make apt take nginx 1.30.7 from nginx.org and no other version. The pin has a priority above
// 1000, so apt installs that version even over a newer one, and the hold keeps it from moving at all.
func nginxOrgSources(t osinfo.Target) (list, prefs string) {
	list = "# Written by mpn.\ndeb [signed-by=" + nginxKeyring + "] https://nginx.org/packages/" + t.ID + " " + t.Codename + " nginx\n"
	prefs = "# Written by mpn.\nPackage: *\nPin: origin nginx.org\nPin-Priority: 900\n\n" +
		"Package: nginx\nPin: version " + NginxVersion + "-1~" + t.Codename + "\nPin-Priority: 1001\n"
	return list, prefs
}

// nginxInstallActions installs nginx, from nginx.org at the fixed version or from the distribution, and holds it there.
func nginxInstallActions(t osinfo.Target) []Action {
	if !t.NginxOrg() {
		return []Action{aptInstall("nginx"), Run("apt-mark", "hold", "nginx")}
	}
	list, prefs := nginxOrgSources(t)
	return []Action{
		aptInstall("gnupg"),
		Run("sh", "-c", nginxOrgKeyScript),
		WriteFile("/etc/apt/sources.list.d/nginx.list", list, 0o644),
		WriteFile("/etc/apt/preferences.d/99-mpn-nginx", prefs, 0o644),
		aptUpdate(),
		aptInstall("nginx=" + NginxVersion + "-1~" + t.Codename),
		Run("apt-mark", "hold", "nginx"),
		// The modules of the panel load only into this exact build, so any other nginx stops the install here, not later when a module
		// fails to load.
		Run("sh", "-c", `v=$(nginx -v 2>&1); case "$v" in *nginx/`+NginxVersion+`*) ;; *) echo "nginx `+NginxVersion+` was expected, this machine has: $v" >&2; exit 1;; esac`),
		// The panel's nginx.conf includes both folders, which this package does not make.
		Run("install", "-d", "-m", "0755", nginxModulesEnabled, "/etc/nginx/sites-enabled"),
		Run("rm", "-f", "/etc/nginx/conf.d/default.conf"),
	}
}

// pageSpeedStep makes the PageSpeed module available to nginx: from the build of this project on the systems with nginx.org, and
// from the repository of We-Amp (built for the distribution's own nginx) on a system that keeps its distribution's nginx.
func pageSpeedStep(t osinfo.Target) (Step, error) {
	if !t.NginxOrg() {
		return Step{"nginx pagespeed", []Action{
			Run("sh", "-c", pagespdAptScript),
			aptUpdate(),
			aptInstall("nginx-module-pagespeed"),
			Run("nginx", "-t"),
			ReloadNginx(),
			pageSpeedCacheDir(),
		}}, nil
	}
	acts, err := moduleActions(t, ModPageSpeed)
	if err != nil {
		return Step{}, err
	}
	acts = append(acts,
		Run("nginx", "-t"),
		ReloadNginx(),
		pageSpeedCacheDir(),
	)
	return Step{"nginx pagespeed", acts}, nil
}

// pageSpeedCacheDir is the cache of PageSpeed, which nginx's workers write.
func pageSpeedCacheDir() Action {
	u := distro.WebUser()
	return Run("install", "-d", "-o", u, "-g", u, "-m", "0755", "/var/cache/ngx_pagespeed")
}

// ModsecBaseline is /etc/nginx/modsecurity.conf for a system whose nginx comes from nginx.org: the settings the OWASP rules assume,
// which the Debian package of the connector ships as a file of its own and the library does not. Without SecRequestBodyAccess the
// body of a POST is never looked at, and the other limits are the ones ModSecurity recommends.
const ModsecBaseline = `# Written by mpn: the base settings ModSecurity recommends, which the rules of the OWASP Core Rule Set assume.
SecRequestBodyAccess On
SecRequestBodyLimit 13107200
SecRequestBodyNoFilesLimit 131072
SecRequestBodyLimitAction Reject
SecResponseBodyAccess Off
SecTmpDir /tmp/
SecDataDir /tmp/
SecPcreMatchLimit 1000
SecPcreMatchLimitRecursion 1000
SecArgumentSeparator "&"
SecCookieFormat 0
SecStatusEngine Off
`

// WAFPackagesPlan installs what the web application firewall needs. nginxVersion is the version of the nginx that is installed now
// ("" when unknown): the module built for nginx.org is used only on a machine that really runs that version, so a machine installed
// before the version was fixed keeps getting the module of its distribution.
func WAFPackagesPlan(t osinfo.Target, nginxV string) (Plan, error) {
	// nginxV is the whole output of "nginx -v": a distribution's nginx at the pinned version (Ubuntu 26.04 before 1.2.3 could have
	// had one) still needs the distribution's module.
	nginxVersion, distroNginx := NginxVersionOf(nginxV), DistroNginx(nginxV)
	if t.EL() {
		acts, err := elWAFActions(t)
		if err != nil {
			return nil, err
		}
		return Plan{{Name: "ModSecurity packages", Actions: acts}}, nil
	}
	update := Run("sh", "-c", "apt-get -o DPkg::Lock::Timeout=300 update || true")
	if !t.NginxOrg() || nginxVersion != NginxVersion || distroNginx {
		return Plan{{Name: "ModSecurity packages", Actions: []Action{update, AptInstall("libnginx-mod-http-modsecurity", "modsecurity-crs")}}}, nil
	}
	mod, err := moduleActions(t, ModModSecurity)
	if err != nil {
		return nil, err
	}
	// The library is called libmodsecurity3t64 on the newer systems and libmodsecurity3 on the older ones.
	apt := "DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=300 install -y "
	lib := Run("sh", "-c", apt+"libmodsecurity3t64 modsecurity-crs || "+apt+"libmodsecurity3 modsecurity-crs")
	acts := append([]Action{update, lib}, mod...)
	acts = append(acts, WriteFile("/etc/nginx/modsecurity.conf", ModsecBaseline, 0o644))
	return Plan{{Name: "ModSecurity packages", Actions: acts}}, nil
}

// NginxVersionOf reads the version out of the output of "nginx -v" ("nginx version: nginx/1.30.7"), or "".
func NginxVersionOf(out string) string {
	_, v, ok := strings.Cut(out, "nginx/")
	if !ok {
		return ""
	}
	if i := strings.IndexAny(v, " \r\n"); i >= 0 {
		v = v[:i]
	}
	return v
}
