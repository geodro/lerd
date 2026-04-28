# Architecture

All containers join the rootless Podman network `lerd`. Communication between Nginx and PHP-FPM uses container names as hostnames.

::: info Platform requirement
On Linux, lerd requires systemd. Every container runs as a Podman Quadlet (systemd unit), every worker as a systemd user service, and the autostart flow uses systemd user linger. Non-systemd distros (OpenRC, runit, s6, sysvinit) are not supported. On macOS the same responsibilities are handled by launchd, which lerd manages automatically.
:::

## Request flow

```
                          *.test DNS
                              │
                   ┌──────────┴──────────┐
                   │   DNS resolver      │
                   │ (NM or resolved)    │
                   └──────────┬──────────┘
                              │ forwards .test queries
                   ┌──────────┴──────────┐
                   │      lerd-dns       │
                   │  (dnsmasq, :5300)   │
                   └──────────┬──────────┘
                              │ resolves to 127.0.0.1
                              ▼
  Browser ──── port 80/443 ──▶ lerd-nginx
                              (nginx:alpine)
                                  │
                      fastcgi_pass :9000
                                  │
                                  ▼
                            lerd-php84-fpm
                          (locally built image)
                                  │
                              reads (bind mount)
                                  │
                                  ▼
                       ~/Lerd/my-app (or any path)
```

## Components

| Component | Technology |
|---|---|
| CLI | Go + Cobra, single static binary |
| Web server | Podman Quadlet (`nginx:alpine`) |
| PHP-FPM | Podman Quadlet per version (locally built image with all Laravel extensions) |
| PHP CLI | `php` binary inside the FPM container (`podman exec`) |
| Composer | `composer.phar` via bundled PHP CLI |
| Node | [fnm](https://github.com/Schniz/fnm) binary, version per project |
| Services | Podman Quadlet containers |
| DNS | dnsmasq container + NetworkManager or systemd-resolved integration |
| TLS | [mkcert](https://github.com/FiloSottile/mkcert), locally trusted CA |

## Key design decisions

**Rootless Podman**: all containers run without root privileges. The only operations requiring `sudo` are DNS setup (configures NetworkManager or systemd-resolved to route `.test` queries) and the initial `net.ipv4.ip_unprivileged_port_start=80` sysctl.

**Dual-stack networking**: the lerd podman bridge is created with both an IPv4 and an IPv6 ULA subnet (`fd00:1e7d::/64`) when the host has a usable IPv6 address (anything outside `::1` and `fe80::/10`). On hosts that advertise IPv6 in the kernel but have no routable v6 on any interface — typical in headless QEMU/KVM VMs, containers, and networks without v6 DHCP — netavark can't reliably hold the ULA gateway on the rootless bridge, so aardvark-dns fails to bind `[fd00:1e7d::1]:53` and service containers exit on start. Lerd detects this by reading `/proc/net/if_inet6` and `/proc/sys/net/ipv6/conf/all/disable_ipv6`, and when no usable v6 is present the `lerd` network is created v4-only instead. Existing networks whose schema doesn't match the current host (dual-stack on a v6-less host, or v4-only on a host that now has v6) are recreated in place on the next `lerd install`: attached containers stop, the network is recreated with the right schema, the previous `network_dns_servers` list is restored, and the containers restart. When dual-stack is in use, nginx vhosts listen on `0.0.0.0` and `[::]`, lerd-dns answers AAAA records for `*.test` (`::1` locally, the host's primary global v6 when `lerd lan:expose on`), and every managed `PublishPort` in service quadlets is paired (a `127.0.0.1:5432` bind also gets a `[::1]:5432`). To opt out, set an explicit subnet via `podman network create` before `lerd install` runs, or override the `lerd-*` quadlets to remove the `[::]` / `[::1]` lines before they're written.

Binding symmetry is preserved across stacks: `127.0.0.1` maps to `[::1]` and `0.0.0.0` maps to `[::]`, so a loopback-only service on v4 stays loopback-only on v6. Services bound through pasta (quadlets without a `Network=` line) remain v4-only because pasta can't bind v6 ports in the current version. Two pitfalls to be aware of: host firewall rules that only filter IPv4 (iptables without matching ip6tables, or a firewall UI that only surfaces v4) leave v6 ports reachable even when the equivalent v4 rule blocks them; and `lerd lan:expose on` will answer AAAA with the host's primary global-unicast v6, which on a SLAAC-addressed LAN can be reachable beyond the v4 NAT boundary. Both are covered in more detail under [Security caveats](../usage/remote-development.md#security-caveats).

**Podman Quadlets**: containers are defined as systemd unit files (`.container` files) managed by the Quadlet generator. This means `systemctl --user start lerd-nginx` works like any other systemd service, and containers restart on failure and at login.

**Shared nginx**: a single nginx container serves all sites via virtual hosts. nginx uses a Podman-network-aware resolver to route `fastcgi_pass` to the correct PHP-FPM container by hostname.

**Per-version PHP-FPM**: each PHP version gets its own container built from a local `Containerfile`. The image includes all extensions needed for Laravel out of the box: `pdo_mysql`, `pdo_pgsql`, `bcmath`, `mbstring`, `xml`, `zip`, `gd`, `intl`, `opcache`, `pcntl`, `exif`, `sockets`, `redis`, `imagick`.

**Automatic volume mounts**: the PHP-FPM and nginx containers bind-mount `$HOME` by default. When a project lives outside the home directory (e.g. `/var/www`, `/opt/projects`), lerd automatically adds the extra volume mount to both containers and restarts them. This happens transparently during `lerd link`, `lerd park`, or the first `lerd php` / `composer` / `laravel new` invocation from the outside path.
