---
title: Background services
---

# Background services

Background services supervise long-running workers such as queue consumers. They run under the website's Linux account and have their own systemd unit, status and journal. They do not replace a site's web process and do not receive its `PORT` setting. Use the [Cron tab](cron.md) for commands that should run and exit on a schedule.

Open a website's **Background services** tab to add a service. New services are saved stopped; start one when it is ready. Each service can be started, stopped, restarted, edited, removed, and inspected through its recent logs. Removing a service stops and disables its unit before removing its unit file and definition.

The background service type follows the website's app type. Each website offers one matching type:

- **PHP command** on PHP websites. Enter the command and arguments, for example `artisan queue:work --sleep=3 --tries=3`. MCPN uses the site's configured PHP CLI version and runs from the site root.
- **Binary** on Binary websites. Enter a relative executable path under the site's account home followed by any arguments, for example `bin/worker --queue default`. The executable must resolve to a file inside the home directory and be executable.
- **mise task** on mise websites. Enter a task defined in the site's `mise.toml`. MCPN checks the task at save and start time and runs `mise install` before starting the task.

Static and reverse-proxy websites do not offer background services because they have no matching app runtime.

Commands use a quoted argument list, not a shell. Shell operators, substitutions and expansions are rejected; quote an argument containing spaces. For example, use `artisan queue:work --queue="mail queue"`. Do not add `&` or shell redirections. Output goes to the system journal; open **Recent logs** for the latest 100 lines.

An optional memory limit applies to that service alone. `0` means no per-service limit. Services include systemd's `NoNewPrivileges`, `PrivateTmp`, restart-on-failure behavior and per-site user isolation.

Delegated users need **View background services** to see the tab and **Manage background services** to create, edit, start, stop, restart or remove services. Unset permissions are denied.
