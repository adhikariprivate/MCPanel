---
title: SSH/SFTP
---

# SSH/SFTP

Open **SFTP** from the website sidebar or select the **SSH/SFTP access** key icon in the website page header.

Use it to upload files and manage the website from the command line or SFTP software.

## Connection details

The card shows **Host**, **Port**, **Account** and **State** (On / Off). Click a field to copy its value.

## Enabling login

Select the **Enable access** power icon in the page header. The panel generates a random password and shows it **only once** on the result page. The panel stores it nowhere, so copy it right away.

## Resetting the password

Select the **Reset password** circular-arrow icon in the page header and confirm the reset. This stops the old password working immediately. SFTP software and anyone using the account must enter the new one, which is also shown only once.

## Disabling login

Select the **Disable access** icon in the page header and confirm. This locks the password: the account can no longer sign in through SSH or SFTP. The website keeps running. Enabling login again creates a new password.

While login is off, the [Terminal](terminal.md) and the panel’s file manager are unavailable too.

## The panel’s key

The terminal and file manager sign in with a separate key, so they do not need the password above. The key shows one of three states:

* **Can sign in**: normal.
* **Cannot sign in, restore it**: click **Restore key** and the panel reinstalls the key.
* **Not checked**.
