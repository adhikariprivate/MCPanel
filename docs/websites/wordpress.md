---
title: WordPress and PHP applications
---

# WordPress and PHP applications

## Installing an application

For a PHP website, either when creating it ([Adding a website](adding-a-website.md)) or later from the Overview tab, you can choose an application for the panel to download and configure:

| Code | Document root | Notes |
| --- | --- | --- |
| **WordPress** | `htdocs` | Adds a **WordPress** tab for management |
| **Laravel** | `public` | Creates `.env` and a database |
| **CodeIgniter 4** | `public` | Creates `.env` and a database |
| **CakePHP** | `webroot` |  |
| **Yii 2** | `web` | Runs in production mode |
| **phpBB** | `htdocs` | Forum. Creates a database; you finish in phpBB’s installer (see below) |

Each one needs a minimum PHP version. If the website uses an older PHP, the panel says so and you change the version in the Overview tab.

* The web directory must be **empty**. The panel never overwrites your files.
* Code is downloaded from its official source and verified by checksum.
* For WordPress, enter the title, admin name, email and language. The admin password is generated randomly and shown **once** on the result page. Installation takes one to a few minutes.
* If installation fails, the panel cleans up what it downloaded, the website remains, and you can retry from the Overview tab.

Choose “Empty, I’ll upload my own code” to upload code yourself over SFTP or the Files tab.

## phpBB

The panel downloads phpBB 3.3.19 from phpbb.com, checks its SHA-256, creates a database and writes an nginx configuration made for phpBB: pretty URLs through `app.php`, PATH\_INFO, and the folders `config`, `cache`, `files`, `includes`, `store` and `vendor` refused to visitors.

phpBB sets itself up in its own installer, so the panel stops one step earlier than for WordPress:

1. The result page shows the database name, user and password. Save them.
2. Open `http://your-domain/install/`. At the database step choose **MySQL with MySQLi**, server `localhost`, port empty, and the name, user and password from the result page.
3. Finish the installer and create the administrator.

Within a minute of the administrator being made, the panel removes the `/install/` location from nginx and deletes the `install` folder. Until then the Overview tab shows a link to the installer.

## WordPress tab

Available only for websites where the panel installed WordPress. The panel runs WP-CLI as the website’s own account, so files always keep the right owner. [Back up](backups.md) before updating.

* **Version**, address and an **Open wp-admin** link. When a newer release exists, an **Update to x.y.z** button appears.
* **Install a plugin or theme**: search wordpress.org and click **Install**, or enter a package name (slug) such as `contact-form-7`. Installs only from wordpress.org.
* Installed **Plugins** and **Themes**: see version and status, and **Activate / Deactivate / Update / Delete**. Deleting also removes the item’s files.
