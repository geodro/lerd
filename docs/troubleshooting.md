# Troubleshooting

When something isn't working, start with the built-in diagnostics:

```bash
lerd doctor   # full check: podman, systemd, DNS, ports, images, config
lerd status   # quick health snapshot of all running services
```

`lerd doctor` reports OK/FAIL/WARN for each check with a hint for every failure.

---

??? bug "`.test` domains not resolving"
    Run the DNS check first:

    ```bash
    lerd dns:check
    ```

    If it fails, restart NetworkManager and check again:

    ```bash
    sudo systemctl restart NetworkManager
    lerd dns:check
    ```

    On systems using systemd-resolved (Ubuntu), check that the per-interface DNS configuration was applied:

    ```bash
    resolvectl status
    # Look for your default interface — it should show 127.0.0.1:5300 as DNS server
    # and ~test as a routing domain
    ```

??? bug "Nginx not serving a site"
    Check that nginx and the PHP-FPM container are running, then inspect the generated vhost:

    ```bash
    lerd status                         # check nginx and FPM are running
    podman logs lerd-nginx              # nginx error log
    cat ~/.local/share/lerd/nginx/conf.d/my-app.test.conf   # check generated vhost
    ```

??? bug "PHP-FPM container not running"
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

??? bug "Permission denied on port 80/443"
    Rootless Podman cannot bind to ports below 1024 by default. Allow it:

    ```bash
    sudo sysctl -w net.ipv4.ip_unprivileged_port_start=80
    # Make permanent:
    echo 'net.ipv4.ip_unprivileged_port_start=80' | sudo tee /etc/sysctl.d/99-lerd.conf
    ```

    `lerd install` sets this automatically, but it may need to be re-applied after a kernel update.

??? bug "Watcher service not running"
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

??? bug "HTTPS certificate warning in browser"
    The mkcert CA must be installed in your browser's trust store. Ensure `certutil` / `nss-tools` is installed, then re-run `lerd install`:

    - Arch: `sudo pacman -S nss`
    - Debian/Ubuntu: `sudo apt install libnss3-tools`
    - Fedora: `sudo dnf install nss-tools`

    After installing the package, run `lerd install` again to register the CA.

??? bug "Error: NetworkUpdate is not supported for backend CNI: invalid argument"
    Your system is likely configured to use the older CNI backend, which lacks support for the requested network operation. Edit or create the Podman configuration file at `/etc/containers/containers.conf` and add or modify the `network_backend` setting to `netavark`:

    ```toml
    [network]
    network_backend = "netavark"
    ```

    To ensure a clean switch and recreate the networks with the new backend, reset the Podman storage. **Warning**: this will wipe all existing containers, pods, and networks:

    ```bash
    podman system reset
    ```
