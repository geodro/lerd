# Troubleshooting

When something isn't working, start with the built-in diagnostics:

```bash
lerd doctor   # full check: podman, systemd, DNS, ports, images, config
lerd status   # quick health snapshot of all running services
```

`lerd doctor` reports OK/FAIL/WARN for each check with a hint for every failure.

---

::: details `.test` domains not resolving
Run the DNS check first:

```bash
lerd dns:check
```

If it fails, restart your DNS resolver and check again:

```bash
# NetworkManager systems:
sudo systemctl restart NetworkManager

# systemd-resolved only (e.g. omarchy):
sudo systemctl restart systemd-resolved

lerd dns:check
```

On systems using systemd-resolved, check that the DNS configuration was applied:

```bash
resolvectl status
# Look for your default interface — it should show 127.0.0.1:5300 as DNS server
# and ~test as a routing domain
```
:::

::: details Nginx not serving a site
Check that nginx and the PHP-FPM container are running, then inspect the generated vhost:

```bash
lerd status                         # check nginx and FPM are running
podman logs lerd-nginx              # nginx error log
cat ~/.local/share/lerd/nginx/conf.d/my-app.test.conf   # check generated vhost
```
:::

::: details PHP-FPM container not running
Check the systemd unit status and logs:

```bash
systemctl --user status lerd-php84-fpm
systemctl --user start lerd-php84-fpm
podman logs lerd-php84-fpm
```

If the image is missing (e.g. after `podman rmi`):

```bash
lerd php:rebuild
```
:::

::: details Permission denied on port 80/443
Rootless Podman cannot bind to ports below 1024 by default. Allow it:

```bash
sudo sysctl -w net.ipv4.ip_unprivileged_port_start=80
# Make permanent:
echo 'net.ipv4.ip_unprivileged_port_start=80' | sudo tee /etc/sysctl.d/99-lerd.conf
```

`lerd install` sets this automatically, but it may need to be re-applied after a kernel update.
:::

::: details Watcher service not running
The watcher monitors parked directories, site config files, git worktrees, and DNS health. If sites aren't being auto-registered or queue workers aren't restarting on `.env` changes:

```bash
lerd status                            # shows watcher running/stopped
systemctl --user start lerd-watcher   # start it from the terminal
# or use the Start button in the UI → System → Watcher
```

To see what the watcher is doing:

```bash
journalctl --user -u lerd-watcher -f
# or open the live log stream in the UI → System → Watcher
```

For verbose output (DEBUG level), set `LERD_DEBUG=1` in the service environment:

```bash
systemctl --user edit lerd-watcher
# Add:
# [Service]
# Environment=LERD_DEBUG=1
systemctl --user restart lerd-watcher
```
:::

::: details HTTPS certificate warning in browser
The mkcert CA must be installed in your browser's trust store. Ensure `certutil` / `nss-tools` is installed, then re-run `lerd install`:

- Arch: `sudo pacman -S nss`
- Debian/Ubuntu: `sudo apt install libnss3-tools`
- Fedora: `sudo dnf install nss-tools`

After installing the package, run `lerd install` again to register the CA.
:::

::: details PHP image build is slow on first run
lerd normally pulls a pre-built base image from ghcr.io and finishes in ~30 seconds. If you see it fall back to a local build instead, the most common cause is being logged into ghcr.io with expired or unrelated credentials — the registry rejects the authenticated request even though the image is public.

lerd handles this automatically since v1.3.4 by always pulling anonymously. If you are on an older version, running `podman logout ghcr.io` before the build will fix it.
:::

::: details Nginx fails to start (missing certificates)
`lerd start` automatically detects SSL vhosts that reference missing certificate files and repairs them before starting nginx:

- **Registered sites** — the site is switched back to HTTP and the vhost is regenerated. The registry is updated (`Secured = false`).
- **Orphan SSL vhosts** — configs left behind by unlinked sites with missing certs are removed.

Repaired items are printed as warnings during startup:

```
  WARN: missing TLS certificate for myapp.test — switched to HTTP
```

To re-enable HTTPS after the automatic repair, run `lerd secure <name>`.

If nginx still fails to start, check the logs:

```bash
journalctl --user -u lerd-nginx -n 30 --no-pager
```
:::

::: details Port conflicts on `lerd start`
`lerd start` checks for port conflicts before starting containers. If another process is already using a required port, you'll see a warning:

```
Port conflicts detected:
  WARN: port 80 (nginx HTTP) already in use — may fail to start (check: ss -tlnp sport = :80)
```

Common culprits are Apache, another nginx instance, or a previously running lerd that wasn't stopped cleanly. Find and stop the conflicting process:

```bash
ss -tlnp sport = :80    # show what's listening on port 80
```

`lerd doctor` also checks for port conflicts as part of its full diagnostic.
:::

::: details Workers failing or crash-looping
Check `lerd status` — the Workers section lists all active, restarting, or failed workers. In the web UI, failing workers show a pulsing red toggle and a **!** on their log tab.

To inspect the error:

```bash
journalctl --user -u lerd-queue-my-app -f    # or lerd-horizon-my-app, lerd-schedule-my-app
```

Common causes:
- Missing Redis when `QUEUE_CONNECTION=redis` — start it with `lerd service start redis`
- Missing dependencies after a fresh clone — run `lerd setup` to install them
- Bad `.env` values — run `lerd env` to reset service connection settings

When you unlink a site, crash-looping workers are automatically detected and stopped.
:::

::: details Error: NetworkUpdate is not supported for backend CNI: invalid argument
Your system is likely configured to use the older CNI backend, which lacks support for the requested network operation. Edit or create the Podman configuration file at `/etc/containers/containers.conf` and add or modify the `network_backend` setting to `netavark`:

```toml
[network]
network_backend = "netavark"
```

To ensure a clean switch and recreate the networks with the new backend, reset the Podman storage. **Warning**: this will wipe all existing containers, pods, and networks:

```bash
podman system reset
```
:::
