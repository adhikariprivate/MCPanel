---
title: Overview tab
---

# Overview tab

Open a website from the [Websites](index.md) page to land on the **Overview** tab, the default tab of the website detail page.

## Status information

* **Status**: Running / Stopped for Binary and mise services. PHP and static sites are served by shared nginx/PHP-FPM.
* **Disk usage**: total size of the website’s home directory. For very large directories the panel shows “too large to measure quickly” so the page does not hang.
* **SSL certificate**: days remaining, or “No certificate”.
* **Latest backup**: time of the most recent successful backup.

## Settings by website type

### PHP

* **PHP version**: switch quickly between installed versions.
* **Common settings**: `memory_limit`, `max_execution_time`, `upload_max_filesize` and similar. The panel limits each website to the supported PHP settings.
* **Other settings**: less common php.ini options, hidden by default.

### Binary and mise

These applications run as the website account under dedicated systemd services. Binary sites select an executable under the account's home directory. mise sites select a task in the Overview and show the active or pending task before saving it.

### Reverse proxy

* **Upstream address**: the HTTP/HTTPS address Nginx forwards requests to.

## Document root

Defaults to `htdocs`. You can switch to a subdirectory, for example `public` for Laravel. The panel creates the directory if it does not exist.

## Applying changes

Click **Save settings**. The panel rebuilds the configuration and validates it before loading. If validation fails (for example `nginx -t` reports an error), the panel **keeps the running configuration** and shows the detailed error, so the website is not interrupted.

## Deleting a website

The **Delete website** section is at the bottom of the tab and requires typing the website name to confirm. By default it only removes the configuration and unregisters the site from the panel. Tick “Also delete the system account, home directory and all files” to remove everything.
