# MCPN documentation

**MCPN** means **My Control Panel**. MCPN is a lightweight hosting control panel for Linux servers. The existing `mpn` command and service names remain for compatibility.

![MCPN — My Control Panel](websites/assets/logo.svg){ width="88" }

## Guides

- [Websites overview](websites/index.md): the website list and the actions available there.
- [Compiled binary web apps](binary-apps.md): deploy Go, Rust and other executable web servers.
- [Changelog](changelog.md): release history.

## Build these docs locally

Install the Material for MkDocs theme and run:

```sh
python3 -m pip install -r requirements-docs.txt
mkdocs serve
```

Build a static copy with `mkdocs build`; output is written to `site/`.
