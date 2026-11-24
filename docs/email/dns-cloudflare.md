---
title: Adding DNS through Cloudflare
---

# Adding DNS through Cloudflare

If the mail domain uses Cloudflare’s name servers, the panel can add the DNS records (MX, SPF, DKIM, DMARC) for you, so you do not copy them by hand. The **Add DNS through Cloudflare** button sits next to the domain on the [Email](index.md) page or the website’s Email tab.

## Create an API token

1. Open Cloudflare’s API token page.
2. Choose **Create Token** and use the **Edit zone DNS** template.
3. Under **Zone Resources**, select the domain’s zone. Required permissions: **Zone · DNS · Edit** and **Zone · Zone · Read**.
4. Create the token, copy it, paste it into the panel’s **API token** field and click **Add records**.

The token is used for this one run and is **not stored** on the server. Delete the token in Cloudflare afterwards.

## Result for each record

* **Added**
* **Already exists**: left as is.
* **Differs from the existing record**: another record sits in this spot, and the panel neither overwrites nor deletes it. Check and edit it in Cloudflare if you want to change it.
* **Error**

If the mail server name has no A record and the machine has a public IP, the panel also adds an A record **without Cloudflare’s proxy**, since mail does not go through the proxy.

## Common errors

| Message | What to do |
| --- | --- |
| Cloudflare rejected the token | Check that the whole token was copied |
| Token lacks permission | Needs Zone · DNS · Edit and Zone · Zone · Read |
| No zone found for this domain | Select the right zone when creating the token |
| Cloudflare is rate limiting | Retry after a minute |
