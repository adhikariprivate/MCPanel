---
title: Websites overview
description: Overview of the Websites page in MCPN.
---

# Websites overview

The **Websites** page is the panel’s main page — it lists every website currently running on the server.

![Websites list page with one running PHP website, demo-com](assets/websites.png)

## Information shown for each website

- **Name**: the website’s system name (used as the SSH/SFTP account name and home directory name).
- **Type**: PHP, Node.js, Python, Binary app, Static or Reverse proxy.
- **Domain**: the primary domain; extra domains collapse into “+N other domains”.
- **Details**: the runtime version (PHP 8.4, Node 22…) or the upstream address (for reverse proxy).
- **SSL**: whether a TLS certificate is installed.

## Website types

| Type | Used for | Default document root |
| --- | --- | --- |
| **PHP** | WordPress, Laravel, custom PHP code | `htdocs` (or `public` when using a PHP framework) |
| **Node.js** | Long-running Node apps (Express, Next.js…) | the directory chosen at creation |
| **Python** | Flask, Django, FastAPI, running in their own virtualenv | the directory chosen at creation |
| **Binary app** | A compiled Go, Rust or other web server, supervised by systemd | `htdocs` for public files; executable stored elsewhere in the site home |
| **Static** | Plain HTML/CSS/JS, no server-side processing | `htdocs` |
| **Reverse proxy** | Forwards requests to another address (an app running on a different host/port) | not applicable |

Each type has different settings tabs once you open a website’s detail page — see [Adding a website](https://mpanel.biz/websites/add/) and [Overview tab](https://mpanel.biz/websites/overview-tab/).

## What you can do from here

- [Add a new website](https://mpanel.biz/websites/add/).
- Click a website’s name to open its detail page, with tabs: Overview, Databases, Backups, and more. The SSH/SFTP settings are in Overview.
- **Apply**: reload the website’s Nginx configuration (useful after a manual edit, or if the running configuration seems out of sync with saved settings).
- **Remove**: delete the website from the server (with a confirmation step before it happens).
