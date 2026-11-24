# MCPN documentation

**MCPN** means **My Control Panel**. MCPN is a lightweight hosting control panel for Linux servers. The existing `mpn` command and service names remain for compatibility.

![MCPN — My Control Panel](websites/assets/logo.svg){ width="88" }

## Documentation

- [Introduction](getting-started/introduction.md) and [installation](getting-started/installation.md) explain what MCPN does and how to install it.
- [Website guides](websites/index.md) cover website setup, files, runtime settings, backups, SSL and logs.
- [Email](email/index.md), [monitoring](monitoring/server-monitoring.md), [firewall](firewall.md), [services](services.md), [security](security.md), and [account settings](account-settings.md) explain the remaining panel features.
- [Compiled binary web apps](binary-apps.md) and [mise websites](mise-sites.md) cover executable app deployments.
- [Changelog](changelog.md) records release history.

## Build these docs locally

Install the Material for MkDocs theme and run:

```sh
python3 -m pip install -r requirements-docs.txt
mkdocs serve
```

Build a static copy with `mkdocs build`; output is written to `site/`.
