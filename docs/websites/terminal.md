---
title: Terminal
---

# Terminal

The **Terminal** tab opens a command-line session in the browser, signed in as the website’s own system account. The session has only that account’s permissions and cannot touch other websites.

The panel does not store anything you type or see in the terminal.

## Requirements

The terminal needs SSH/SFTP login enabled for the website, see [SSH/SFTP](ssh-sftp.md). If it is off, open **SFTP** and use the **Enable access** power icon in the header. You can open **Terminal** from the sidebar or use the **Terminal** icon in a website page header.

## Session states

* **Connecting…** then **Connected**.
* **Session ended**: you typed `exit` or the session was closed.
* **Could not connect**: usually the panel’s key is broken. Open **SFTP** and click **Restore key**.

The terminal and the file manager connect over SSH to the server itself, on the port sshd listens on. A port other than 22, set by your provider or changed in [Firewall › Service ports](../firewall.md#service-ports), is found automatically.

The number of terminals open at once is limited. If the panel reports too many terminals, close older sessions and try again.
