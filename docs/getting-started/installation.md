---
title: Installation
---

# Installation

## Requirements

* A VPS running Debian 12/13, Ubuntu 22.04/24.04/26.04, or AlmaLinux, Rocky Linux, CentOS Stream 9/10; x86\_64 or arm64 (aarch64) architecture.
* At least 1 GB RAM.
* Root access (or `sudo`).
* A public IP address; a domain name is optional and can be added later.

## Installing

SSH into the VPS as root, then run:

```
curl -fsSL https://raw.githubusercontent.com/adhikariprivate/MCPanel/main/scripts/installer/install.sh | sh
```

The installer will:

1. Check the operating system and architecture, and stop if they aren’t supported.
2. Download the `mpn` program from the [latest GitHub release](https://github.com/adhikariprivate/MCPanel/releases/latest) and compare its SHA-256 with the `SHA256SUMS` file. If they don’t match, it stops and installs nothing.
3. Install Nginx 1.30.7 (a pinned version from nginx.org) with the pagespeed module. The ModSecurity module, built for this exact Nginx, is installed only when you turn on the web application firewall on the [Security](../security.md) tab, because it uses more RAM.
4. Install the panel as two services, `mpn-web` (unprivileged, listening on a Unix socket only Nginx can open) and `mpn-root` (the privileged helper), with a self-signed TLS certificate so the panel serves HTTPS from the start.
5. Print instructions and the **setup link**, in the form `https://<your-ip>:2610/setup?t=…`. The link carries a one-time key for creating the administrator account.

Installation takes a few minutes depending on network speed.

The installer does **not** install PHP, Node.js or a database. Pick what your websites need on the [Services](../services.md) page after signing in: PHP 7.0 to 8.5, Node.js 18 to 24, one of MariaDB or MySQL, PostgreSQL, and phpMyAdmin for MariaDB/MySQL.

### Options

| Variable | Meaning |
| --- | --- |
| `MPN_VERSION` | Install a fixed release, such as `1.0.0`. The default is the latest release. |
| `MPN_PANEL_PORT` | HTTPS port of the panel. The default is `2610`. |

Example:

```
curl -fsSL https://raw.githubusercontent.com/adhikariprivate/MCPanel/main/scripts/installer/install.sh | MPN_VERSION=1.0.0 sh
```

To read the installer before running it:

```
curl -fsSLO https://raw.githubusercontent.com/adhikariprivate/MCPanel/main/scripts/installer/install.sh

less install.sh

sh install.sh
```

## AlmaLinux, Rocky Linux and CentOS Stream

On these systems MCPN does a few things differently from Debian and Ubuntu:

* Nginx 1.30.7 comes from the nginx.org repository and is locked with `dnf versionlock`, so `dnf upgrade` leaves it alone.
* PHP 7.4 and newer come from the Remi repository (it needs EPEL, which the installer turns on). PHP 7.0 to 7.3 and the ModSecurity module are built by the project and checked by SHA-256.
* MariaDB comes from rpm.mariadb.org and MySQL from repo.mysql.com. Version 10 has MySQL 8.4 only, because repo.mysql.com does not publish MySQL 8.0 for it. PostgreSQL comes from the PostgreSQL Yum Repository.
* **SELinux stays on.** The panel labels the website folders, the logs, the PHP-FPM sockets and its own ports, so there is no need to turn SELinux off.
* **firewalld is turned off.** The panel manages the firewall with nftables on the [Firewall](../firewall.md) page.
* PHP 7.0 to 7.3 on version 10 have no `intl` extension.

## After installing

1. Open the setup link the installer printed and create the administrator account. See [First-run setup](first-run-setup.md).
2. Go to the [Services](../services.md) page to install PHP, Node.js and a database as you need.
3. [Add a website](../websites/adding-a-website.md), point its domain at the server’s IP, then turn on [SSL](../websites/ssl.md).

If your VPS provider has its own firewall, open TCP ports 80 and 443 and the panel port (2610 by default).

Caution

The installer does **not** create an administrator account. The setup page opens only with the link that carries the key, so someone who knows the panel address still cannot create the account. Keep the link to yourself and create the account right away. If you lose it, sign in over SSH and run `mpn web setup-link`.

## Updating

From 1.2.0 you update inside the panel: click **Update** in the main menu, then click **Check for updates** and then **Update**. From a terminal, run:

```
mpn update -apply
```

The panel installs a release only when its signature matches the key built into the program, so a file altered on the way is refused. The running program is kept as `/usr/local/bin/mpn.prev` and put back if the panel does not answer after the update. The panel restarts during the update; reload the page after about a minute.

A server on 1.1.x or older does not have this command yet: run the install command once more. The installer downloads the latest release, replaces the `mpn` and `mpn-cloud` programs and runs `mpn panel migrate` to bring the configuration up to date. Websites, databases and the administrator account are kept.
