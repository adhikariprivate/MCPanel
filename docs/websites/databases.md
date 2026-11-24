---
title: Databases
---

# Databases

The **Databases** tab manages a website’s databases. Each database has its own user, which can access only that database.

## List

The table lists each **Database and user** (they share one name) with the actions **phpMyAdmin**, **Change password** and **Delete**.

## Adding a database

The **Name (optional)** field:

* Blank: uses the website name, with `-` changed to `_`.
* A suffix such as `shop`: becomes `<website-name>_shop`.
* Only lowercase a-z, digits and underscores. The name is at most 32 characters, including the website prefix.

Click **Add database**. The password is shown **once**, right after creation. The system does not store it, so save it.

## Changing the password

The current password stops working immediately. The website’s config file (`wp-config.php`, `.env`…) must be updated with the new one. The new password is also shown only once.

## Deleting

This deletes the database with all its data and its user, and **cannot be undone**. [Back up](backups.md) first if the data matters.

## phpMyAdmin

Click **phpMyAdmin** to open an admin session in a new tab. The sign-in uses a temporary account with rights on that database only, which expires after 30 minutes or when you log out of phpMyAdmin. The database’s real password is untouched and stored nowhere.
