---
title: mise websites
description: Manage a site's runtimes and supervised web task with mise.
---

# mise websites

A **mise app** is a website whose tools and tasks are managed by [mise](https://mise.jdx.dev/). MCPN installs a pinned mise executable at `/usr/local/bin/mise`. Each website has private mise metadata at `/var/lib/mpn/mise/<site>/data` and runtimes at `/var/lib/mpn/mise/<site>/installs`; a site cannot read or change another site's runtimes.

## Create and configure

Create a mise app from **Websites**. MCPN creates the site's Linux account and a starter `mise.toml` at `/home/<site>/htdocs/mise.toml`. This fixed document root keeps the config reachable in the site's file tools. The starter contains a harmless one-shot `demo` task; no systemd service is created or started until you select a task.

Use the site's file tools to edit `mise.toml`. Declare the runtime your application needs under `[tools]`. mise can manage Python, Node.js, and other supported tools; MCPN treats them as site-owned runtimes and does not provide separate Python or Node.js website controls. For example, choose the relevant tool for your application:

```toml
[tools]
node = "22"
# Or use Python instead:
# python = "3.12"

[tasks.serve]
description = "Run the web server"
run = "exec node server.js"
```

Add another supported mise tool in the same `[tools]` table when your application needs it. Use a long-running task that starts the web server and binds to the supplied `PORT`.

Save the file and open the site's Overview. In **mise web task**, choose a task from the list. The card shows the saved task as **Active**, previews the command (`mise run <task>`), and identifies the site account that runs it. A changed selection is marked **Pending save**; **Discard** restores the saved selection. **Save & reload service** stores only the task choice and restarts the service. Choose **Do not run a task** to stop the service. MCPN runs `mise install` and `mise run <task>` as the site's Linux account. Runtime installation happens when systemd starts the service; a failed install or task start appears in the service status and journal. The task list is also read as the site account. MCPN never loads a site's configuration or runs its task as root.

If the current task was removed from `mise.toml`, the card marks it as missing and lets you select a remaining task or stop the service. If MCPN cannot read the task list, it still lets you stop the service; a new task cannot be selected until the list can be read again.

Treat `mise.toml`, tasks, plugins, and application code as executable site-owner code. A task can use every file and process permission the site's account has.

## Web process requirements

The selected task must start a long-lived server, stay in the foreground, and bind to `127.0.0.1:$PORT`. MCPN sets `PORT` to the allocated loopback TCP port and nginx forwards web requests to it. Keep the process in the foreground; do not daemonize it or bind a public interface. A task that exits successfully is not a running web server. The starter `demo` task is only an example and does not listen for requests.

MCPN restarts a failed service and can apply a per-site memory limit. The task's stdout and stderr are available in the system journal for `app-<site>.service`.

## Protected files and SELinux

Nginx denies requests for `mise.toml`, `mise.*.toml`, and the conventional `mise-tasks/` directory. Dotfiles remain denied by the normal site rule. The config is still editable through MCPN's file tools.

On AlmaLinux, Rocky Linux, and CentOS Stream, MCPN labels the site's nonreplaceable `installs` directory before the site account installs any runtimes. New files inherit that executable label. systemd runs MCPN's fixed pre-start helper with elevated privileges; the helper invokes `mise install` as the site's Linux account and never traverses or relabels site-writable runtime contents as root. It does not run the site's task or configuration as root. MCPN labels conventional mise task-script directories narrowly. Legacy Node.js and Python site services are removed by the breaking migration while home-directory contents are preserved.
