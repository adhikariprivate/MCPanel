# Compiled binary web apps

MCPN can supervise a compiled web server, such as one written in Go or Rust, as a website type. Each binary site runs as its own `app-<site>.service` systemd unit under its site account. The unit restarts the process after a failure and can apply a memory limit.

## Deploy a binary

1. Create a **Binary app** website in the Websites page. MCPN allocates a TCP port and passes it to the process in the `PORT` environment variable.
2. Build a Linux executable for the server's CPU architecture. Configure the program to listen on `127.0.0.1:$PORT`.
3. Upload it to the website account's home directory, outside the public `htdocs` directory. The default path is `bin/server`.
4. Make the executable runnable (`chmod +x bin/server`) and apply the website again. The systemd unit starts the program once the executable is present.

Nginx forwards requests to the allocated loopback port. The binary runs under the site's own system account, and its logs are available from the site's Logs tab. Keep the executable and configuration files owned by that site account.

The current form supports a single executable path, the `PORT` variable and an optional memory limit. It does not configure command-line arguments or additional environment variables.
