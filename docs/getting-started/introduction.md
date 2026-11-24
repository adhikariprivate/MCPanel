---
title: Introduction
---

# Introduction

MCPN (`mpn`) is a web server admin panel written in Go, shipped as a single binary. It’s built for low-spec VPS machines (starting at 1 GB RAM), without requiring containers or a separate Go runtime.

## What MCPN manages

* **Websites**: PHP, Node.js, Python, static, reverse proxy — each type has its own settings tab.
* **Databases**: MariaDB or MySQL, each database with its own dedicated user.
* **Email**: Postfix + Dovecot + OpenDKIM, with per-domain webmail.
* **SSL**: Let’s Encrypt with automatic renewal, or your own certificate.
* **Backups**: nightly automatic backups, latest 7 kept, copied off-site to Google Drive, S3, Backblaze B2, SFTP, WebDAV or FTP.
* **Firewall**: uses nftables and keeps the SSH and panel ports open in its default rules.
* **Security**: ModSecurity + OWASP CRS, fail2ban against password guessing.
* **Monitoring**: CPU, memory, disk and network in real time.

## What MCPN is for

MCPN is for people who run one or a few small VPS machines themselves and want to host websites, databases and email on them without typing commands for every task. The panel handles the everyday work: creating websites, issuing SSL certificates, backups, opening firewall ports and reading logs.

MCPN is designed to use little memory. The panel itself needs neither MySQL nor PHP. Installation requires a VPS with at least 1 GB of RAM. Actual memory needs depend on the websites and services you run.

## Architecture overview

The panel is split into two separate parts:

1. **`mpn-web`** — runs without root privileges, serves the HTTP(S) interface only.
2. **`mpn-root`** (helper) — runs as root, executes the real system commands (creating websites, installing services…).

For privileged system operations, the web interface sends a `ctl.Request` to the root helper. Separating privileges reduces the web process’s attack surface.

## Next steps

* [Install](installation.md) on your VPS.
* [First-run setup](first-run-setup.md) to create the administrator account.
