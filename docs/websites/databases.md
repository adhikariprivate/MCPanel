---
title: Databases
---

# Databases

The **Databases** tab manages a website’s databases. Each database has its own user or role, which can access only that database.

## List

The table lists each database and its engine. The database and its dedicated user/role share one name. MariaDB/MySQL databases also have a **phpMyAdmin** action; PostgreSQL databases do not.

## Adding a database

The **Name (optional)** field:

* Blank: uses the website name, with `-` changed to `_`.
* A suffix such as `shop`: becomes `<website-name>_shop`.
* Only lowercase a-z, digits and underscores. The name is at most 32 characters, including the website prefix.

Click **Add database**. The password is shown **once**, right after creation. The system does not store it, so save it.

Choose the database engine from the engines installed on the server. MariaDB and MySQL are alternatives; PostgreSQL can run alongside either one. Use a matching database driver in your application. Built-in application installers currently provision MariaDB/MySQL databases; for PostgreSQL, create the database here and configure your application to use it.

For PHP sites, MCPN installs the PostgreSQL driver (`pgsql`, including PDO_pgsql) when PostgreSQL is installed and for PHP versions added afterwards. On AlmaLinux, Rocky Linux and CentOS Stream, the custom PHP 7.0–7.3 builds do not include a separately installable PostgreSQL driver; use PHP 7.4 or later for PostgreSQL connections on those systems.

## Changing the password

The current password stops working immediately. The website’s config file (`wp-config.php`, `.env`…) must be updated with the new one. The new password is also shown only once.

## Deleting

This deletes the database with all its data and its user/role, and **cannot be undone**. [Back up](backups.md) first if the data matters.

## phpMyAdmin

For MariaDB/MySQL, click **phpMyAdmin** to open an admin session in a new tab. The sign-in uses a temporary account with rights on that database only, which expires after 30 minutes or when you log out of phpMyAdmin. The database’s real password is untouched and stored nowhere. PostgreSQL has no phpMyAdmin button.
