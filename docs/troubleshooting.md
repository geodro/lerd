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
# Look for your default interface, it should show 127.0.0.1:5300 as DNS server
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

::: details My custom nginx directive disappeared after an update
Don't edit `~/.local/share/lerd/nginx/conf.d/*.conf` directly. Lerd regenerates those files on `lerd link`, `lerd secure`, `lerd site rebuild`, and every `lerd install` (which `lerd update` re-execs). Drop your snippet in `~/.local/share/lerd/nginx/custom.d/{domain}.conf` instead — the generated vhost ends with an `include` for that file, and lerd never writes into `custom.d/`. See [Nginx Overrides](./usage/nginx-overrides.md) for examples.
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

::: details `podman exec` fails with "chdir: No such file or directory"
This happens when your project is outside your home directory (e.g. `/var/www/`, `/opt/projects/`). The PHP-FPM and nginx containers only mount `$HOME` by default.

Lerd handles this automatically: when you `lerd link`, `lerd park`, or run any exec command (`lerd php`, `composer`, `laravel new`) from an outside path, lerd adds the volume mount and restarts the affected containers.

If you see this error on an older lerd version, update to the latest and re-link the site:

```bash
lerd update
lerd unlink && lerd link
```

To verify the mounts are in place:

```bash
grep Volume ~/.config/containers/systemd/lerd-nginx.container
grep Volume ~/.config/containers/systemd/lerd-php*-fpm.container
```

You should see your project path listed alongside the `%h:%h` mount.
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
# or use the Start button in the UI under System > Watcher
```

To see what the watcher is doing:

```bash
journalctl --user -u lerd-watcher -f
# or open the live log stream in the UI under System > Watcher
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
lerd normally pulls a pre-built base image from ghcr.io and finishes in ~30 seconds. If you see it fall back to a local build instead, the most common cause is being logged into ghcr.io with expired or unrelated credentials; the registry rejects the authenticated request even though the image is public.

lerd handles this automatically since v1.3.4 by always pulling anonymously. If you are on an older version, running `podman logout ghcr.io` before the build will fix it.
:::

::: details Nginx fails to start (missing certificates)
`lerd start` automatically detects SSL vhosts that reference missing certificate files and repairs them before starting nginx:

- **Registered sites**: the site is switched back to HTTP and the vhost is regenerated. The registry is updated (`Secured = false`).
- **Orphan SSL vhosts**: configs left behind by unlinked sites with missing certs are removed.

Repaired items are printed as warnings during startup:

```
  WARN: missing TLS certificate for myapp.test, switched to HTTP
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
  WARN: port 80 (nginx HTTP) already in use, may fail to start (check: ss -tlnp sport = :80)
```

Common culprits are Apache, another nginx instance, or a previously running lerd that wasn't stopped cleanly. Find and stop the conflicting process:

```bash
ss -tlnp sport = :80    # show what's listening on port 80
```

`lerd doctor` also checks for port conflicts as part of its full diagnostic.
:::

::: details Workers missing after reinstall
If you ran `lerd uninstall` and then reinstalled, worker units and service quadlets are deleted during uninstall. Running `lerd start` after reinstalling automatically restores them from the `workers` list saved in each site's `.lerd.yaml`. If `.lerd.yaml` does not exist or was not committed, you will need to start workers again manually (`lerd queue:start`, etc.).

To check what was restored:
```bash
lerd status   # shows all active workers and services
```
:::

::: details Workers failing or crash-looping
Check `lerd status`, the Workers section lists all active, restarting, or failed workers. In the web UI, failing workers show a pulsing red toggle and a **!** on their log tab.

To inspect the error:

```bash
journalctl --user -u lerd-queue-my-app -f    # or lerd-horizon-my-app, lerd-schedule-my-app
```

Common causes:
- Missing Redis when `QUEUE_CONNECTION=redis`, start it with `lerd service start redis`
- Missing dependencies after a fresh clone, run `lerd setup` to install them
- Bad `.env` values, run `lerd env` to reset service connection settings

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

::: details Error: unable to parse ip fe80::...%18 specified in AddDNSServer: invalid argument
Your host's DNS configuration includes a zoned link-local IPv6 nameserver, typically advertised by your router via SLAAC + RDNSS. The zone identifier (`%18` is a kernel interface index) is meaningless inside a container's network namespace, and netavark refuses to accept it.

Lerd 1.18+ filters these addresses automatically before handing them to podman. If you're still on 1.17 or older, upgrade with `lerd update` and rerun `lerd install`. The filter is conservative: only zoned link-local (`fe80::...%iface`) addresses are dropped; globally routable IPv6 nameservers (e.g. `2606:4700:4700::1111`) are preserved.

When filtering empties the entire DNS list, lerd falls back to pasta's standard forwarder (`169.254.1.1`), which bridges into the host's resolver and preserves `.test` routing.
:::

::: details Containers can resolve `.test` over IPv4 but not over IPv6
Lerd 1.18+ creates the lerd podman network as dual-stack (v4 + v6) and writes both A and AAAA records for `.test` domains. If you upgraded from an older version, the existing v4-only `lerd` network is migrated automatically the next time you run `lerd install`: attached containers stop, the network is recreated with the `fd00:1e7d::/64` ULA prefix, the previous DNS server list is restored, and the containers restart. Quick check:

```bash
podman network inspect lerd --format '{{.Subnets}}'
# expect both an IPv4 subnet and one starting with fd00:1e7d::
```

If the v6 subnet is missing, run `lerd install` once to migrate. To verify resolution from inside a container:

```bash
podman run --rm --network lerd alpine sh -c 'nslookup laravel.test; nslookup -type=AAAA laravel.test'
```
:::

::: details Services fail to start with "aardvark-dns failed to bind [fd00:1e7d::1]:53"

Symptom: after `lerd install`, a subset of service containers (commonly `lerd-nginx`, `lerd-postgres`, `lerd-meilisearch`) fail to start. Journal shows:

```
Error: netavark: error while applying dns entries: IO error: aardvark-dns failed to start
Error starting server failed to bind udp listener on [fd00:1e7d::1]:53:
IO error: Cannot assign requested address (os error 99)
```

Cause: the host advertises IPv6 in the kernel but has no routable v6 address on any interface — only `::1` and `fe80::` — so netavark can't hold the ULA gateway on the rootless bridge, and aardvark-dns bind fails with `EADDRNOTAVAIL`. Typical in headless QEMU/KVM VMs and networks without v6 DHCP.

Lerd 1.18+ detects this on every `lerd install` by reading `/proc/net/if_inet6` (any non-loopback, non-link-local v6 address counts as usable) and falls back to a v4-only `lerd` network. An existing dual-stack network on a v6-less host is recreated as v4-only automatically. Force it:

```bash
lerd install
# look for: "Recreated lerd network as v4-only (host has no usable IPv6)."
```

If the host later gains v6 connectivity, the next `lerd install` will recreate the network as dual-stack again.
:::

::: details Every DNS lookup inside a lerd container stalls ~5 seconds
Symptom: pages that hit the database or any container-to-container hostname feel slow, and `time dig <anything> @<container>` takes roughly five seconds before returning an answer. The network looks fine in `podman network inspect lerd` (both IPv4 and IPv6 subnets present), but aardvark-dns's on-disk config has the v6 gateway absent from its listen-ips line.

Cause: `podman network rm` doesn't clean up `$XDG_RUNTIME_DIR/containers/networks/aardvark-dns/<name>` between rm and recreate, so a network that was originally v4-only can leave aardvark with a v4-only listen header even after the network is recreated dual-stack. The container's `/etc/resolv.conf` still lists the v6 gateway as the primary nameserver, queries to it time out (~5s), then glibc falls back to the v4 gateway.

Lerd 1.18+ detects this drift on `lerd install` (aardvark listen line is v4-only despite the network being dual-stack) and self-heals by recreating the network with the stale aardvark state wiped. If you're on an earlier 1.18 build or the heal didn't fire, force it:

```bash
lerd install
```

Manual verification:

```bash
cat "${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/containers/networks/aardvark-dns/lerd" | head -1
# expect both gateways, e.g.: fd00:1e7d::1,10.89.7.1 169.254.1.1
# if only 10.89.7.1 is present, the drift fix didn't run — re-run lerd install
```
:::
