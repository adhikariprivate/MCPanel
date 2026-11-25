---
title: Backups
---

# Backups

The **Backups** tab manages backups of a website’s files and databases.

## Automatic backups

Every night at about 03:30 each website is backed up and the **7 newest copies** are kept. They live on this server (`/var/backups/mpn`), so they **do not protect you if the server is lost**. The panel shows this nightly backup policy to every site user.

## Backup list

Each row shows **Time**, **State** (Complete / Incomplete / Unreadable), **Content** (Files, DB) and **Size**.

## Backup now

Use **Back up** in the page actions to create a manual copy, for example before updating WordPress. This action is available to the instance administrator.

## Restoring

Click **Restore** on the copy you want, then choose files, database or both.

Caution

Restoring replaces matching files and database tables on the server with the backed-up versions. Files and tables created after the backup remain. If the current state matters, take a fresh backup first.

## Deleting old copies

The **Delete old copies** section keeps the number of newest copies you choose and removes the rest. You must type the website name to confirm.

## Off-site backup

After each nightly backup, the new copy is sent to the storage you choose, so backups survive a server failure. The connection is shared by all websites; each website turns copying on or off separately.

| Storage | What you need |
| --- | --- |
| **Google Drive** | Your own OAuth client (see below). The panel sees only the files it creates. |
| **S3** (Cloudflare R2, AWS, Wasabi…) | Access key ID, secret access key, bucket; endpoint and region depend on the provider. For Cloudflare R2 the endpoint is `https://<account id>.r2.cloudflarestorage.com`. |
| **Backblaze B2** | Key ID and application key, ideally limited to one bucket. The bucket must exist. |
| **SFTP** | Host, port, user, a password or a private key, and the **host key** as `ssh-ed25519 AAAA…` (get it with `ssh-keyscan -t ed25519 <host>` on a trusted machine). |
| **WebDAV** (Nextcloud, ownCloud…) | The WebDAV address and an account. For Nextcloud the address is `https://<host>/remote.php/dav/files/<user>/`; use an app password. |
| **FTP** | Host, port, user. TLS (FTPS) is on by default; turn it off only when the FTP server lacks it, because the password and data then cross the network unencrypted. |

### Connecting

1. An instance administrator opens **Off-site backups** in the main sidebar, picks a type under **Choose the storage**, and fills in the fields. This connection and its credentials are shared by all websites.
2. Click **Connect and test**. The panel writes a small file, reads it back and deletes it; the connection is saved only when all three work. Otherwise it tells you which step failed and saves nothing.
3. For each website, delegated users with the off-site backup manage permission can choose **Enable copying** or click **Copy now** to start a background copy. The instance administrator controls the shared destination and completes Google Drive authorization.

For Google Drive, in Google Cloud Console enable the Google Drive API, configure the OAuth consent screen, then create an **OAuth client ID** of type **TVs and Limited Input devices**. Enter the **Client ID** and **Client secret**, click **Connect Google Drive**, open the address shown, sign in to Google and enter the code, then return and click **I have authorized access**. The panel asks only for the `drive.file` scope.

Backups are stored under `mpn-backups/<website>/<time>/`. The storage keeps the 7 newest copies and older ones are removed automatically. A copy that is cut off continues with what is missing next time.

Note

Backups contain both the database and the files, so copy them only to storage you trust. The storage credentials can be read only by root.

### Restoring from off-site storage

The off-site backup view permission lets delegated users list the website’s copies with **View backups in the storage**. **Download to server** requires the off-site backup manage permission. The download runs in the background. The panel checks every file and accepts only a backup signed by this machine (see below). Once it is downloaded, restore it from the list at the top of the tab like any other backup.

### Backup signatures

From 1.2.0, each backup’s MANIFEST records the SHA-256 of every file and an HMAC signature made with the machine’s own key, `/etc/mpn/backup.key` (readable by root only). Someone who can change the storage still cannot slip a forged backup onto your server.

Caution

Backups made with 1.1.x or older are not signed, so they cannot be downloaded from off-site storage. Copies on the server restore as before. When moving to a new server, copy `/etc/mpn/backup.key` over as well if you want to download the old copies there.

**Disconnect** deletes the storage credentials from the server (for Google Drive it also revokes access at Google). Copies already in the storage stay there. This applies to all websites.
