---
title: Adding a website
---

# Adding a website

The website page groups controls into tabs. **Overview** shows the current status and website settings. **Domain** contains domain and SSL controls, **SFTP** contains connection and account controls, and **Advanced** contains vHost configuration, acceleration and website deletion. The Email tab and Webmail setting appear only while the instance email service is enabled.

From the [Websites](index.md) page, select the **Add website** plus icon in the page header, or go directly to `/sites/new`.

![Add website form with name, type and domain fields](../assets/mpanel/sites-new.DqnqyoeC_2cDRNx.webp)

## Fields to fill in

* **Name**: lowercase letters, numbers and hyphens, up to 31 characters. This also becomes the system account name (SSH/SFTP) and the home directory name.
* **Type**: PHP, Binary, mise, Static or Reverse proxy — see each type’s details in [Websites overview](index.md).
* **Domain**: one domain, e.g. `example.com`. Configure `www` and extra domains afterwards in the Overview tab.
* Type-specific fields:
  * **PHP**: PHP version (only versions already installed via [Services](../services.md) are listed).
  * **Binary**: executable path under the site's home and a port.
  * **mise**: select a task in the site's Overview after adding it.
  * **Reverse proxy**: upstream address (HTTP or HTTPS) to forward requests to.

## SSH/SFTP account

Each website gets its own system account. Leave the password blank to let the panel generate one randomly, or type your own (12–64 characters, no whitespace, quotes or backticks).

Caution

The password appears **only once**, on the result page right after creation. The panel never stores it anywhere — copy and save it immediately.

## After creating

Click **Create and apply**. The panel will:

1. Create the system account (if it doesn’t exist yet).
2. Create the home directory and web directory (`htdocs`, or the directory you specified).
3. Generate the Nginx configuration and apply it right away (`nginx -t` runs before reloading).
4. For PHP: provision a dedicated PHP-FPM pool for the website.
5. For Binary and mise sites: create a dedicated systemd service that starts automatically with the server.

Node.js and Python are no longer managed site types. When upgrading a machine that has these site types, review `mpn panel migrate` first. The migration keeps each site's `/home/<user>` directory and detaches its database records so they remain manageable.

If applying the migration manually, stop `mpn-root` and `mpn-web` first and start them again after it completes. The installer and in-panel updater handle this automatically.

The migration removes MCPN's obsolete Node.js release keyring. Shared runtimes under `/opt/node` are left in place because preserved site files or custom services may still use them.

For Python, Node.js, or another language runtime, create a **mise** website and declare the runtime in its `mise.toml`; MCPN does not install or select those runtimes as website settings. See the [mise website guide](../mise-sites.md) for a Python and Node.js example.

If something’s wrong (e.g. the name is already taken, or the PHP version isn’t installed), the panel reports the error right on the form and creates nothing.

## Next steps

* Open the new website to see its [Overview tab](overview-tab.md).
* For PHP websites, you can install WordPress or a PHP framework right from the Overview tab — see [WordPress](wordpress.md).
* Add [SSL](ssl.md) once the domain’s DNS points to this server.
