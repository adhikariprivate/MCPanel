---
title: Users and site access
---

# Users and site access

The instance administrator can create panel users and assign each user to selected websites. Open **Users and access** from the sidebar, create a username and initial password, choose a starting role preset, select the websites, and set each permission to **Allow**, **Deny**, or **Inherit**. The **Access** link in a website header shows who is assigned to that website.

The **Read only**, **Developer**, and **Site manager** presets fill in a starting set of permissions for newly created users. Choosing a preset updates the per-site controls so you can review and change each permission before saving. They are not fixed roles: the saved result is a set of per-site grants. Terminal and SSH access are never included by a preset.

Permissions are grouped by website area. Each control shows a plain-language action and its capability key. **Inherit** uses the selected starting preset during creation; when editing an existing user, it means deny unless a permission is explicitly allowed.

The first account created during [first-run setup](getting-started/first-run-setup.md) remains the instance administrator. It can manage all websites, users, services, security settings, upgrades, and other instance-wide controls. Delegated users cannot grant access or promote themselves.

## Permission rules

- A user must be enabled and assigned to a website before any site permission applies.
- Assignment grants basic overview access (`site.view`) so the user has a landing page. Other permissions are independent.
- An explicit **Allow** grants the named capability. **Deny** blocks it. **Inherit** uses the selected starting preset, or denies access when no preset applies. Unlisted capabilities are denied.
- The panel checks permissions on every authenticated request. Hiding a page or button is only a usability aid; a direct URL or crafted form is still checked on the server.
- Saving a user's access changes terminates that user's active sessions. Disabled users cannot sign in, and their current sessions are removed.

## Available site capabilities

The current per-site controls cover overview, site settings, vHost viewing and editing, file reading and changes, SSL, scheduled jobs, logs, database listing, backup listing, site email, terminal and SSH access, WordPress tools, and cloud backup viewing/sync controls. Delegated users with `site.view` on at least one assigned site can open Activity from the main sidebar; its site filter and results are limited to sites assigned to that user.

Terminal access gives the user a shell as the website's Linux account, with the same file and process access as that account. SSH management can enable or disable that account's system login or change its panel key. Grant these only to people who should have that level of control over the website.

Database listing and backup listing are currently read-only for delegated users. Database administration, backup creation/restoration/deletion, site creation/removal, and all instance-wide pages remain instance-administrator-only. Any route without an explicit site permission mapping is denied to delegated users.

## Account limitations

Delegated users can change their own password from **Account settings** in the sidebar. Their password is stored in the delegated access file and changing it does not affect the instance administrator account. Two-factor setup remains available to the instance administrator only; delegated users do not have a separate 2FA enrollment flow.

User creation, updates, permission changes, disablement, and denied high-risk requests are recorded in the activity log. Passwords and hashes are never included in those entries.
