---
title: Overview tab
---

# Overview tab

Open a website from the [Websites](index.md) page to land on the **Overview** tab, the default tab of the website detail page.

## Status information

* **Status**: Running / Stopped (for Node.js and Python. PHP and static sites are always “running” because they are served by the shared nginx/PHP-FPM).
* **Disk usage**: total size of the website’s home directory. For very large directories the panel shows “too large to measure quickly” so the page does not hang.
* **SSL certificate**: days remaining, or “No certificate”.
* **Latest backup**: time of the most recent successful backup.

## Settings by website type

### PHP

* **PHP version**: switch quickly between installed versions.
* **Common settings**: `memory_limit`, `max_execution_time`, `upload_max_filesize` and similar. The panel limits each website to the supported PHP settings.
* **Other settings**: less common php.ini options, hidden by default.

### Node.js

* **Node.js version**
* **Entry file**: path relative to the document root, e.g. `server.js` or `dist/index.js`.
* **Application port**: leave it empty and the app listens on the Unix socket `/run/mpn-app/<website>/app.sock`, which only Nginx can reach (the default for new websites from 1.2.0). The path is in the `SOCKET` environment variable, and `PORT` holds the same path, so Node.js’s `server.listen(process.env.PORT)` keeps working. Enter a port number and the app listens on `127.0.0.1` at `PORT`. On AlmaLinux, Rocky Linux and CentOS Stream apps always use a port.
* **Maximum RAM**: an application that exceeds this limit is stopped and restarted automatically.

### Python

* **Python version**
* **Entry file**: a path, or `-m module_name`. The app runs in the website’s own virtualenv (`~/venv`).
* **Application port**: as for Node.js, empty means a Unix socket. A Python app reads the path from `SOCKET`, for example `gunicorn --bind unix:$SOCKET app:app`.

### Reverse proxy

* **Upstream address**: the HTTP/HTTPS address Nginx forwards requests to.

## Document root

Defaults to `htdocs`. You can switch to a subdirectory, for example `public` for Laravel. The panel creates the directory if it does not exist.

## Applying changes

Click **Save settings**. The panel rebuilds the configuration and validates it before loading. If validation fails (for example `nginx -t` reports an error), the panel **keeps the running configuration** and shows the detailed error, so the website is not interrupted.

## Deleting a website

The **Delete website** section is at the bottom of the tab and requires typing the website name to confirm. By default it only removes the configuration and unregisters the site from the panel. Tick “Also delete the system account, home directory and all files” to remove everything.
