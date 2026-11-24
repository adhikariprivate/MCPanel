#!/bin/sh
# MCPN (My Control Panel) installer.
#
#   curl -fsSL https://raw.githubusercontent.com/adhikariprivate/MCPanel/main/scripts/installer/install.sh | sh
#
# Installs nginx with the PageSpeed module and MCPN itself (ModSecurity is added when switched on). PHP and the database are not installed here:
# pick the ones you want later on the Services page of the panel. Use mise websites for Node.js, Python, or other app runtimes.
#
# Optional environment variables:
#   MPN_VERSION    release to install, such as 1.0.0 (default: the latest release)
#   MPN_PANEL_PORT HTTPS port of the panel (default: 2610)
set -eu

REPO="adhikariprivate/MCPanel"
# The MPN_ prefix matters: /etc/os-release, sourced below, sets VERSION, ID and others.
MPN_WANT="${MPN_VERSION:-}"
MPN_PORT="${MPN_PANEL_PORT:-2610}"

say()  { printf '\033[1;36m==>\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31mError:\033[0m %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "run this as root (for example: curl -fsSL https://raw.githubusercontent.com/adhikariprivate/MCPanel/main/scripts/installer/install.sh | sudo sh)"
[ "$(uname -s)" = "Linux" ] || die "MCPN runs on Linux only"
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "unsupported CPU $(uname -m); MCPN needs x86_64 or arm64 (aarch64)" ;;
esac
command -v systemctl >/dev/null 2>&1 || die "systemd is required"

[ -r /etc/os-release ] || die "cannot read /etc/os-release"
# shellcheck disable=SC1091
. /etc/os-release
# AlmaLinux and Rocky Linux give the minor release in VERSION_ID (9.8, 10.2), CentOS Stream only the major one.
MPN_FAMILY=debian
case "${ID:-}:${VERSION_ID:-}" in
  ubuntu:22.04|ubuntu:24.04|ubuntu:26.04|debian:12|debian:13) ;;
  almalinux:9|almalinux:9.*|almalinux:10|almalinux:10.*|rocky:9|rocky:9.*|rocky:10|rocky:10.*|centos:9|centos:10) MPN_FAMILY=el ;;
  *) die "unsupported system ${PRETTY_NAME:-unknown}; supported: Ubuntu 22.04, 24.04, 26.04, Debian 12, 13, AlmaLinux, Rocky Linux and CentOS Stream 9, 10" ;;
esac
case "$MPN_PORT" in ''|*[!0-9]*) die "MPN_PANEL_PORT must be a number" ;; esac

if ! command -v curl >/dev/null 2>&1 || ! command -v sha256sum >/dev/null 2>&1; then
  say "Installing curl"
  if [ "$MPN_FAMILY" = el ]; then
    # The images ship curl-minimal, which gives the curl command; asking for curl would conflict with it.
    dnf -y -q install ca-certificates coreutils || die "dnf could not install coreutils"
  else
    DEBIAN_FRONTEND=noninteractive apt-get update -qq
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq curl ca-certificates coreutils
  fi
fi

case "$MPN_WANT" in ''|v[0-9]*|[0-9]*) ;; *) die "MPN_VERSION must look like 1.0.0" ;; esac
case "$MPN_WANT" in *[!0-9A-Za-z.v-]*) die "MPN_VERSION must look like 1.0.0" ;; esac
if [ -n "$MPN_WANT" ]; then
  BASE="https://github.com/$REPO/releases/download/v${MPN_WANT#v}"
else
  BASE="https://github.com/$REPO/releases/latest/download"
fi

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

say "Downloading MCPN ($ARCH)"
curl -fsSL --proto '=https' --retry 3 -o "$TMP/mpn" "$BASE/mpn_linux_$ARCH"
curl -fsSL --proto '=https' --retry 3 -o "$TMP/SHA256SUMS" "$BASE/SHA256SUMS"
want=$(awk -v f="mpn_linux_$ARCH" '$2 == f { print $1 }' "$TMP/SHA256SUMS")
got=$(sha256sum "$TMP/mpn" | cut -d' ' -f1)
[ -n "$want" ] || die "SHA256SUMS has no entry for mpn_linux_$ARCH"
[ "$want" = "$got" ] || die "checksum mismatch for mpn_linux_$ARCH (expected $want, got $got); nothing was installed"
chmod 0755 "$TMP/mpn"
# From 1.2.0 the copy to off-site storage is a second program, mpn-cloud (mpn with rclone). An older release has none.
HAVE_CLOUD=0
if awk -v f="mpn-cloud_linux_$ARCH" '$2 == f { found = 1 } END { exit !found }' "$TMP/SHA256SUMS"; then
  curl -fsSL --proto '=https' --retry 3 -o "$TMP/mpn-cloud" "$BASE/mpn-cloud_linux_$ARCH"
  want=$(awk -v f="mpn-cloud_linux_$ARCH" '$2 == f { print $1 }' "$TMP/SHA256SUMS")
  got=$(sha256sum "$TMP/mpn-cloud" | cut -d' ' -f1)
  [ "$want" = "$got" ] || die "checksum mismatch for mpn-cloud_linux_$ARCH (expected $want, got $got); nothing was installed"
  chmod 0755 "$TMP/mpn-cloud"
  HAVE_CLOUD=1
fi
say "MCPN $("$TMP/mpn" version), checksum OK"

if [ -x /usr/local/bin/mpn ] && [ -f /etc/systemd/system/mpn-web.service ]; then
  # Already installed: replace the program only. Running the whole plan again would rewrite settings chosen since
  # (the phpMyAdmin port in mpn-web.service, for one).
  say "MCPN is already installed ($(/usr/local/bin/mpn version)); updating the program"
  cp -p /usr/local/bin/mpn "$TMP/mpn.previous"
  if [ "$HAVE_CLOUD" = 1 ] && [ -x /usr/local/bin/mpn-cloud ]; then cp -p /usr/local/bin/mpn-cloud "$TMP/mpn-cloud.previous"; fi
  install -m 0755 "$TMP/mpn" /usr/local/bin/mpn
  [ "$HAVE_CLOUD" = 1 ] && install -m 0755 "$TMP/mpn-cloud" /usr/local/bin/mpn-cloud
  if ! systemctl stop mpn-root mpn-web; then
    install -m 0755 "$TMP/mpn.previous" /usr/local/bin/mpn
    if [ -f "$TMP/mpn-cloud.previous" ]; then install -m 0755 "$TMP/mpn-cloud.previous" /usr/local/bin/mpn-cloud; fi
    systemctl restart mpn-root mpn-web || true
    die "could not stop MCPN services for the migration; the previous program was restored and a service restart was attempted"
  fi
  # Bring the files a first install writes up to date. An older program has no migrate subcommand and prints panel usage;
  # never treat an actual migration error as success just because both historically used exit 2.
  MIGRATE_LOG="$TMP/migrate.log"
  rc=0
  /usr/local/bin/mpn panel migrate -apply >"$MIGRATE_LOG" 2>&1 || rc=$?
  if [ "$rc" -eq 2 ] && grep -Eq '^usage: mpn panel <[^>]+>' "$MIGRATE_LOG" && ! grep -Eq '^usage: mpn panel <[^>]*migrate' "$MIGRATE_LOG"; then
    : # Old binary: this subcommand did not exist yet.
  elif [ "$rc" -eq 3 ]; then
    cat "$MIGRATE_LOG" >&2
    rm -f "$MIGRATE_LOG"
    systemctl restart mpn-root mpn-web || true
    die "the database schema migration committed, so the new program was kept. Resolve the follow-up error and restart MCPN services"
  elif [ "$rc" -ne 0 ]; then
    cat "$MIGRATE_LOG" >&2
    rm -f "$MIGRATE_LOG"
    install -m 0755 "$TMP/mpn.previous" /usr/local/bin/mpn
    if [ -f "$TMP/mpn-cloud.previous" ]; then install -m 0755 "$TMP/mpn-cloud.previous" /usr/local/bin/mpn-cloud; fi
    systemctl restart mpn-root mpn-web || true
    die "mpn panel migrate failed; the previous program was restored and a service restart was attempted. Resolve the cause, then run the update again"
  fi
  rm -f "$MIGRATE_LOG"
  systemctl restart mpn-root mpn-web || die "migration completed, but MCPN services did not restart; start mpn-root and mpn-web after resolving the service error"
  say "Updated to $(/usr/local/bin/mpn version)"
  exit 0
fi

say "Installing nginx, its modules and the panel (this takes a few minutes)"
[ "$HAVE_CLOUD" = 1 ] && install -m 0755 "$TMP/mpn-cloud" /usr/local/bin/mpn-cloud
"$TMP/mpn" plan -engine none -panel -panel-port "$MPN_PORT" -apply

IP=$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{ for (i = 1; i < NF; i++) if ($i == "src") { print $(i + 1); exit } }')
[ -n "$IP" ] || IP="<server-ip>"
# The setup page opens only with the key in this link (from 1.2.0). An older program has no setup-link command.
SETUP=$(/usr/local/bin/mpn web setup-link -quiet 2>/dev/null || true)
[ -n "$SETUP" ] || SETUP="https://$IP:$MPN_PORT/setup"

cat <<EOF

  MCPN is installed.

  1. Open the panel now and create the administrator account:

       $SETUP

     The certificate is self-signed for now, so the browser shows a warning; continue to the page.
     Keep this link to yourself: it opens the setup page until the account exists.
     Lost it? Run: mpn web setup-link

  2. Install what your websites need on the Services page:
     PHP (7.0 to 8.5), MariaDB or MySQL, phpMyAdmin. Use mise for application runtimes such as Node.js or Python.

  3. Add a website, point its domain to $IP and turn on SSL.

  If your provider has a firewall, open TCP ports 80, 443 and $MPN_PORT.
EOF
if [ "$MPN_FAMILY" = el ]; then
  cat <<EOF
  On this system firewalld was turned off: the panel manages the firewall itself (Firewall page).
  SELinux stays on; the panel labels the folders of the websites for nginx and PHP-FPM.
EOF
fi
cat <<EOF
  Forgot the password later? Run: mpn web passwd
  Documentation: https://github.com/adhikariprivate/MCPanel/tree/main/docs

EOF
