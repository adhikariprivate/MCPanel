---
title: Webmail
---

# Webmail

Webmail lets you read and send mail in the browser. Sign in with the full email address and the mailbox password.

## Server webmail

On the [Email](index.md) page, the **Web mail** section turns webmail on or off for the whole machine. It is a small separate Go program that listens on a port next to the panel’s port and shares the panel’s certificate.

* The program exits after 15 minutes of nobody using it and restarts when someone opens it, so it uses no RAM while idle.
* The first time, the panel downloads the program and verifies its checksum before installing.
* The panel adds a firewall rule for the webmail port when the panel firewall is on.
* The panel must be installed on this machine, because webmail uses its port and certificate.

## Webmail on a website’s domain

The **Webmail on this domain** card in the website’s Overview tab turns on webmail at the website’s own `/webmail/`, with no extra domain or certificate. It also works with extra domains.

Requirements:

* The mail server and webmail are turned on in the Email page.
* The website has [SSL](../websites/ssl.md), because passwords must not be typed over a plain connection.
* If the website uses a [custom vHost](../websites/custom-vhost.md), the panel cannot add it for you. Add the `/webmail/` block to the vHost by hand.

Caution

Webmail shares its origin with the website. If the website has a code-execution vulnerability, someone with webmail open there could have their mail read. Turn it on only for websites you trust.

## Language

Webmail is a separate program, so it does not yet follow the panel’s language switch.
