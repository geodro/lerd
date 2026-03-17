# Changelog

All notable changes to Lerd will be documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Lerd uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [0.1.62] — 2026-03-17

### Fixed

- PHP 8.5 image build no longer fails when the `imagick` PECL extension can't compile against the new PHP API — imagick is installed if available, silently skipped otherwise (redis is unaffected)

---

## [0.1.61] — 2026-03-17

### Fixed

- Domains are now always lowercased — directory names like `MyApp` or custom `--domain MyApp.test` now consistently produce `myapp.test`

---

## [0.1.60] — 2026-03-17

### Fixed

- All container volume mounts now include the `:z` SELinux relabeling option — on Fedora (and other SELinux-enforcing systems) dnsmasq and nginx containers were unable to read their config files, causing DNS and nginx to fail immediately after install
- Home-directory volume mounts (nginx, PHP-FPM) use `--security-opt=label=disable` instead of `:z` to avoid recursively relabeling the user's home directory

---

## [0.1.53] — 2026-03-17

### Fixed

- `lerd install` now configures the system DNS resolver (writes NM dispatcher / applies `resolvectl`) only **after** `lerd-dns` is running — previously applying `resolvectl dns <iface> 127.0.0.1:5300` before the dnsmasq container started routed all DNS through a non-existent server, breaking image pulls with "no such host" / "server misbehaving"

---

## [0.1.52] — 2026-03-17

### Fixed

- DNS resolution on Ubuntu (systemd-resolved + NetworkManager): NM overrides global `resolved.conf` drop-ins via DBUS so the `DNS=127.0.0.1:5300` drop-in had no effect; now installs an NM dispatcher script (`/etc/NetworkManager/dispatcher.d/99-lerd-dns`) that calls `resolvectl dns/domain` per-interface on "up", and applies it immediately to the default interface
- Upstream DNS servers in the dnsmasq config are now detected from the running system (`/run/systemd/resolve/resolv.conf` → `/etc/resolv.conf`, skipping loopback/stub addresses) — no hardcoded IPs
- `lerd-dns.container` now mounts `~/.local/share/lerd/dnsmasq` into the container and uses `--conf-dir` instead of embedding all options in the `Exec` line

---

## [0.1.51] — 2026-03-17

### Fixed

- DNS resolution now works on systems using systemd-resolved (Ubuntu, etc.) — `lerd install` detects whether systemd-resolved is the active resolver and writes `/etc/systemd/resolved.conf.d/lerd.conf` with `DNS=127.0.0.1:5300` and `Domains=~test` instead of configuring NetworkManager's embedded dnsmasq
- `lerd status` PHP version hint no longer shows "8.5" — corrected to "8.4"

---

## [0.1.50] — 2026-03-17

### Fixed

- `install.sh` `--local` binary path is now validated before `check_prerequisites` runs — previously podman not being installed would cause `die "podman is required"` before the file-exists check, making bats test 23 fail in CI

---

## [0.1.49] — 2026-03-17

### Fixed

- `install.sh` `ask()` no longer causes CI test failures under `set -euo pipefail` when `/dev/tty` is unavailable — `read </dev/tty` now has `2>/dev/null || true` so a missing tty is silently treated as "no"

---

## [0.1.48] — 2026-03-17

### Fixed

- All container images now use fully qualified names (`docker.io/library/nginx:alpine`, etc.) — Ubuntu's `/etc/containers/registries.conf` has no unqualified-search registries, causing short names to fail with exit code 125
- `lerd install` now writes the `lerd.test` UI vhost **before** starting nginx so the dashboard is available on the very first start

---

## [0.1.47] — 2026-03-17

### Fixed

- `lerd install` now runs `podman system migrate` after installing podman on a fresh system to initialise Podman's storage before the first rootless container operation

---

## [0.1.46] — 2026-03-17

### Fixed

- Container images are now pre-pulled before `daemon-reload` / service start so the systemd 90 s default timeout is not exceeded on a fresh install pulling large images; `TimeoutStartSec=300` added to both `lerd-nginx.container` and `lerd-dns.container` as an additional safeguard
- `lerd install` no longer prints a spurious nginx reload `[WARN]` — the separate reload step was removed; `RestartUnit` already loads the latest config

---

## [0.1.45] — 2026-03-17

### Fixed

- `install.sh` `ask()` now reads from `/dev/tty` so prompts work correctly when the script is piped to bash (`curl | bash`); a missing tty falls back gracefully
- `install.sh` now aborts with a clear error if `podman` is not found after the prerequisite install step

---

## [0.1.44] — 2026-03-17

### Fixed

- HTTP→HTTPS redirect in SSL vhosts changed from `301` (permanent, browser-cached) to `302` (temporary) so disabling HTTPS is not cached by the browser
- Site domain links in the dashboard now use `https://` when TLS is enabled and `http://` otherwise

---

## [0.1.43] — 2026-03-17

### Fixed

- `lerd install` (and `lerd update`) no longer overwrites SSL vhosts with plain HTTP configs — sites with `secured: true` in `sites.yaml` now have their SSL vhost regenerated in-place during the vhost regeneration step
- Sites table in the dashboard no longer flickers on background poll — the 5 s interval now updates existing row properties in-place instead of replacing the entire array; new/removed sites are still added/removed correctly

---

## [0.1.42] — 2026-03-17

### Added

- Sites tab now auto-refreshes every 5 seconds — PHP version, Node version, TLS status, and FPM running state stay current without a manual reload
- Install Node version UI added to the Services tab — enter a version number and click Install to run `fnm install` in the background

---

## [0.1.41] — 2026-03-17

### Fixed

- `lerd install` now uses `RestartUnit` (instead of `StartUnit`) for all services so a re-run after `lerd update` picks up the new binary and any changed quadlet files
- Installer bats tests updated: `latest_version` mocks updated for the redirect-based version check, `certutil` added to the `--check` prerequisite mock

---

## [0.1.40] — 2026-03-17

### Fixed

- Sites tab now shows the live PHP/Node version detected from disk (`.php-version`, `.lerd.yaml`, `composer.json`) instead of the stale value stored in `sites.yaml`; if the detected version differs, `sites.yaml` is updated automatically

---

## [0.1.39] — 2026-03-17

### Added

- PHP and Node columns in the Sites tab are now dropdowns — selecting a version writes `.php-version` / `.node-version` to the project directory, updates `sites.yaml`, regenerates the nginx vhost, and reloads nginx; available PHP versions come from installed FPM quadlets, Node versions from `fnm list`

---

## [0.1.38] — 2026-03-17

### Fixed

- HTTPS sites no longer return "File not found" — `SecureSite` was constructing a bare `config.Site` with only `Domain` and `PHPVersion`, leaving `Path` empty so the generated SSL vhost had `root /public`; it now receives the full site struct
- `fetchLatestVersion` tests updated to use the redirect-based approach (fixes broken test suite after v0.1.34 change)

---

## [0.1.37] — 2026-03-17

### Fixed

- HTTPS toggle in Sites tab no longer returns "site not found" — the API was looking up sites by name but receiving the full domain; added `FindSiteByDomain` and switched the handler to use it
- HTTPS column now shows a proper toggle switch instead of "On / Off" text buttons

---

## [0.1.36] — 2026-03-17

### Fixed

- `lerd status` no longer warns about all 7 services being inactive — it now only shows services that have a quadlet file on disk (i.e. were intentionally installed); uninstalled services are silently skipped with a single "No services installed" message if none are present

---

## [0.1.35] — 2026-03-17

### Added

- `install.sh` now checks for `certutil` (`nss-tools`) as a prerequisite and offers to install it automatically — without it mkcert cannot register the CA in Chrome/Firefox, causing `ERR_CERT_AUTHORITY_INVALID` on HTTPS sites
- README documents `certutil`/`nss-tools` as a requirement with per-distro package names

---

## [0.1.34] — 2026-03-17

### Fixed

- Version detection in both `lerd update` and `install.sh` no longer uses the GitHub REST API — it now follows the `https://github.com/{repo}/releases/latest` HTML redirect to extract the tag from the URL; this endpoint is not rate-limited (60 req/hour limit on the API was causing "No releases found" / HTTP 403 for anyone who ran the installer more than a few times)

---

## [0.1.33] — 2026-03-17

### Fixed

- `install.sh` `latest_version()` now sends `User-Agent: lerd-installer` and `Accept: application/vnd.github+json` headers — GitHub's API returns 403 for unauthenticated requests without a User-Agent, which the script was silently treating as "no releases found"
- `install.sh` `cmd_uninstall` now dynamically discovers units from quadlet files on disk (same fix as `lerd uninstall`)

---

## [0.1.32] — 2026-03-17

### Fixed

- `lerd uninstall` now stops and disables all services that were enabled at runtime (e.g. mailpit, soketi started from the UI dashboard) — the unit list is now derived dynamically from the quadlet files on disk instead of a hardcoded list, so nothing is left behind
- `lerd uninstall` now also removes `lerd-ui.service` alongside `lerd-watcher.service`

---

## [0.1.31] — 2026-03-17

### Fixed

- `lerd update` no longer fails with "GitHub API returned HTTP 403" — the version check now sends a `User-Agent: lerd-cli` header, which GitHub requires for unauthenticated API requests

---

## [0.1.30] — 2026-03-17

### Fixed

- `lerd update` now restarts the `lerd-ui` systemd service after applying changes so the new binary is immediately picked up without manual intervention

---

## [0.1.29] — 2026-03-17

### Added

- **HTTPS toggle in Sites tab** — the TLS column is now a clickable button; clicking it calls `POST /api/sites/{domain}/secure` or `unsecure`, issues/removes the mkcert certificate, regenerates the nginx vhost, and reloads nginx inline without leaving the UI

### Fixed

- `lerd secure` no longer fails with "renaming SSL config: no such file or directory" — `RemoveVhost` was deleting both the HTTP and SSL config files before the rename; the command now only removes the HTTP config, then renames the SSL one into place
- `.env` Copy button now works on plain HTTP (`lerd.test`) — `navigator.clipboard.writeText` requires HTTPS; added a `document.execCommand('copy')` fallback via a temporary off-screen textarea

---

## [0.1.28] — 2026-03-17

### Added

- **Live logs drawer** — click any site row in the dashboard to open a live streaming log panel at the bottom of the screen showing that site's PHP-FPM container output (`podman logs -f`); lines are colour-coded (red for errors/fatals, yellow for warnings/notices); auto-scrolls with a 500-line buffer; Clear and Close controls in the header
- **Env vars preview in Services tab** — each service card now has a "Show .env / Hide .env" toggle that expands a syntax-highlighted code block with all the `.env` variables for that service, with a one-click Copy button in the header

### Fixed

- Service start from UI no longer fails with "Unit not found" after the first time a service quadlet is written — `handleServiceAction` now retries `StartUnit` up to 5 times with increasing delays (300 ms each) to give the systemd Quadlet generator time to register the new `.service` unit after `daemon-reload`
- Removed stale "Copied to clipboard!" feedback element that was previously separate from the env preview Copy button

---

## [0.1.27] — 2026-03-17

### Fixed

- `lerd update` (and `lerd install`) no longer prompts for sudo if DNS is already configured — `dns.Setup()` now checks whether `/etc/NetworkManager/conf.d/lerd.conf` and `/etc/NetworkManager/dnsmasq.d/lerd.conf` already contain the correct content and skips all sudo steps if so; this makes updating from the UI dashboard work without any password prompt in the common case

---

## [0.1.26] — 2026-03-17

### Fixed

- `lerd.test` proxy vhost no longer uses `resolver` + `set $upstream` — nginx's resolver directive only works with DNS, but `host.containers.internal` is resolved via `/etc/hosts` inside the container; using a static `proxy_pass http://host.containers.internal:7073` lets nginx resolve it correctly at startup

---

## [0.1.25] — 2026-03-17

### Changed

- `lerd update` no longer unconditionally rebuilds PHP-FPM images — it now computes a SHA-256 hash of the embedded Containerfile and only rebuilds if the hash differs from the one stored after the last successful build
- Hash is stored to `~/.local/share/lerd/php-image-hash` after `lerd php:rebuild`, `lerd use <version>`, and `lerd park` (first build)

---

## [0.1.24] — 2026-03-17

### Fixed

- `lerd.test` proxy vhost now uses `host.containers.internal` instead of the Podman network gateway IP — the gateway IP is typically blocked by the host firewall for connections from containers, while `host.containers.internal` is a Podman built-in that always routes to the host correctly

---

## [0.1.23] — 2026-03-17

### Fixed

- Dashboard service start now writes the Quadlet file and reloads systemd before calling `systemctl start`, fixing "Unit not found" error on first use
- Service action errors are now returned as JSON with the error message and last 20 lines of `journalctl` logs
- Frontend shows a loading spinner while toggling, "Started successfully" / "Stopped" flash on success, and an inline error with expandable logs on failure

---

## [0.1.22] — 2026-03-17

### Fixed

- `lerd.test` dashboard now reachable: UI server changed to listen on `0.0.0.0:7073` so nginx (running inside the Podman container) can reach it via the network gateway IP
- `lerd install` now reloads nginx after writing the `lerd.test` proxy vhost so it takes effect immediately without a manual restart
- `lerd.test` is now a reserved domain — `lerd park` silently skips any directory that would resolve to it, `lerd link` returns an error if the resolved domain is reserved

---

## [0.1.21] — 2026-03-17

### Added

- **Lerd dashboard** — browser UI available at `http://lerd.test`, served by `lerd serve-ui` as a persistent systemd user service (`lerd-ui.service`)
- Dashboard shows three tabs: **Sites** (table with domain links, PHP/Node version, TLS badge, FPM status), **Services** (start/stop toggles, copy `.env` button per service), **System** (DNS, nginx, PHP-FPM health, auto-refreshes every 10 seconds)
- **Update flow** built into the UI: "Check for update" button in sidebar checks GitHub releases; if an update is available shows the version and an "Update" button that runs `lerd update`
- `lerd install` now writes and starts `lerd-ui.service` and generates the `lerd.test` nginx reverse proxy vhost; prints `Dashboard: http://lerd.test` on completion
- `lerd start` / `lerd stop` include `lerd-ui` alongside DNS, nginx, and PHP-FPM

---

## [0.1.20] — 2026-03-17

### Changed

- `lerd stop` now also stops all installed services (those with a quadlet file) in addition to DNS, nginx, and PHP-FPM
- `lerd start` now also starts all installed services

---

## [0.1.19] — 2026-03-17

### Added

- `lerd php:rebuild` — force-removes and rebuilds all installed PHP-FPM images; useful after a Containerfile change
- `lerd update` now automatically runs `lerd php:rebuild` after `lerd install` so PHP-FPM image changes (new extensions, config tweaks) are applied on every update

---

## [0.1.18] — 2026-03-17

### Added

- `lerd logs` — show PHP-FPM container logs for the current project (auto-detects version)
- `lerd logs -f` / `--follow` — tail logs in real time
- `lerd logs nginx` — show nginx container logs
- `lerd logs <service>` — show logs for any service (e.g. `lerd logs mailpit`)
- `lerd logs <version>` — show logs for a specific PHP-FPM container (e.g. `lerd logs 8.5`)
- PHP-FPM containers now route all PHP errors to stderr (`catch_workers_output`, `log_errors`, `error_log=/proc/self/fd/2`) so they appear in `podman logs` / `lerd logs`

---

## [0.1.17] — 2026-03-17

### Added

- `mailpit` service — local SMTP server with web UI at `http://127.0.0.1:8025`; catches all outgoing mail from Laravel apps
- `soketi` service — self-hosted Pusher-compatible WebSocket server for Laravel Echo / broadcasting
- PHP 8.5 support — `lerd use 8.5` builds and starts the PHP 8.5 FPM container; default PHP version updated to 8.5

---

## [0.1.16] — 2026-03-17

### Added

- `lerd php [args...]` — runs PHP inside the correct versioned FPM container, detecting version from `.php-version` / `composer.json` / global default
- `lerd artisan [args...]` — shortcut for `lerd php artisan [args]`
- `lerd node [args...]` — runs Node via fnm with auto-detected version
- `lerd npm [args...]` — runs npm via fnm with auto-detected version
- `lerd npx [args...]` — runs npx via fnm with auto-detected version
- `lerd install` now writes `php`, `composer`, `node`, `npm`, `npx` shims to `~/.local/share/lerd/bin/` so commands work directly from the terminal

---

## [0.1.15] — 2026-03-17

### Fixed

- Service `.env` variables now use container hostnames (`lerd-mysql`, `lerd-redis`, etc.) instead of `127.0.0.1` — PHP-FPM runs inside the `lerd` Podman network so `127.0.0.1` resolves to the container's own loopback, not the host

---

## [0.1.14] — 2026-03-17

### Fixed

- nginx `resolver` directive added to `nginx.conf` using the Podman network gateway so upstream container hostnames are re-resolved dynamically after FPM restarts (previously nginx cached the old IP and returned 502)
- `fastcgi_pass` in vhost templates now uses a `$fpm` variable to force use of the resolver
- `lerd install` now regenerates all registered site vhosts so template changes are applied immediately
- PHP-FPM containers now use a locally built image (`lerd-php{version}-fpm:local`) with all Laravel-required extensions pre-installed: `pdo_mysql`, `pdo_pgsql`, `bcmath`, `mbstring`, `xml`, `zip`, `gd`, `intl`, `opcache`, `pcntl`, `exif`, `sockets`, `redis`, `imagick`
- PHP-FPM images are built automatically on first `lerd use <version>` — subsequent runs reuse the cached image

---

## [0.1.13] — 2026-03-17

### Changed

- `lerd service start` / `lerd service restart` — `.env` output is printed without leading whitespace for direct copy-paste

---

## [0.1.12] — 2026-03-17

### Fixed

- `lerd service start <service>` — automatically writes the quadlet file and reloads systemd before starting, so services work on first use without needing a prior `lerd install`

### Changed

- `lerd service start` and `lerd service restart` now print the recommended `.env` variables to add to your Laravel project after the service starts

---

## [0.1.11] — 2026-03-17

### Added

- `lerd start` — start DNS, nginx, and all installed PHP-FPM containers
- `lerd stop` — stop DNS, nginx, and all installed PHP-FPM containers

---

## [0.1.10] — 2026-03-17

### Fixed

- Nginx and PHP-FPM containers now mount the user's home directory so project files are accessible inside the containers
- `nginx.conf` — added `user root;` and changed pid/error_log to writable paths (`/tmp/nginx.pid`, stderr) so nginx starts correctly in rootless Podman without `UserNS=keep-id`
- PHP-FPM pool now runs workers as root (`-R` flag + `zz-lerd.conf` override) so it can read project files in the home directory
- `ensureFPMQuadlet` — always overwrites the quadlet file (previously skipped if it existed, leaving stale configs in place)
- `lerd install` — now regenerates all existing PHP-FPM quadlets so config changes are applied without manual deletion
- `EnsureNginxConfig` — always overwrites `nginx.conf` (previously skipped if file existed)

---

## [0.1.9] — 2026-03-17

### Fixed

- `lerd-dns.container` quadlet template was embedded from the wrong source directory (`internal/podman/quadlets/`) — the file still referenced `andyshinn/dnsmasq` with `Network=host`, causing the DNS container to fail with "Permission denied on port 53"; updated to the Alpine-based dnsmasq on port 5300 via published port
- `dns.Setup()` and `ensureUnprivilegedPorts()` — `sudo` subprocesses now have `Stdin/Stdout/Stderr` connected to the process terminal so password prompts display correctly instead of failing with "a terminal is required"

### Added

- `lerd unpark [directory]` — removes a parked directory and unlinks all sites registered from it

### Changed

- `lerd park` and `lerd link` — directory names with real TLDs (`.com`, `.net`, `.org`, `.io`, `.ltd`, etc.) now have the TLD stripped and remaining dots replaced with dashes before appending `.test` (e.g. `admin.astrolov.com` → `admin-astrolov.test`)
- `lerd use <version>` / `lerd status` — PHP version detection now tracks FPM quadlet files instead of static CLI binaries, so `lerd use 8.4` is immediately reflected in `lerd status`

---

## [0.1.8] — 2026-03-17

### Fixed

- `lerd update` now automatically runs `lerd install` after swapping the binary, so quadlet files, DNS config, sysctl settings and any other infrastructure changes are applied without the user having to run a second command

---

## [0.1.7] — 2026-03-17

### Fixed

- `lerd-dns.container` — removed `Network=host` and `AddCapability=NET_ADMIN` which both fail under rootless Podman; container now runs dnsmasq on port 5300 via a published port (`127.0.0.1:5300:5300`)
- `lerd install` — now checks `net.ipv4.ip_unprivileged_port_start` and automatically sets it to 80 (with sudo) so rootless Podman can bind nginx to ports 80 and 443; also writes `/etc/sysctl.d/99-lerd-ports.conf` to persist across reboots

### Changed

- `lerd status` — every FAIL entry now shows an actionable hint (e.g. `systemctl --user start lerd-nginx`, `lerd service start mysql`, `lerd use 8.4`)

---

## [0.1.6] — 2026-03-17

### Fixed

- `lerd install` was calling `dns.WriteDnsmasqConfig` (writes only the container's local config) instead of `dns.Setup()`, which means `/etc/NetworkManager/conf.d/lerd.conf` and `/etc/NetworkManager/dnsmasq.d/lerd.conf` were never written and NetworkManager was never restarted — causing `*.test` DNS resolution to silently fail
- `dns.Setup()` now prints a clear message before invoking `sudo` so users know why a password prompt appears

---

## [0.1.5] — 2026-03-17

### Fixed

- `install.sh` — definitively fixed the `install: cannot stat '...\033[0m...'` error by refactoring `download_binary` to accept a caller-supplied directory instead of returning a path via stdout; all output now goes directly to the terminal (stderr) and is never captured by command substitution

---

## [0.1.4] — 2026-03-17

### Fixed

- `install.sh` — `install: cannot stat '...\033[0m...'` error: `download_binary` was called inside `$()` command substitution so its `info` output was captured into the `binary` variable along with the path; all UI output in `download_binary` now goes to stderr, leaving only the path on stdout
- `install.sh` — tar extraction errors inside `download_binary` now also go to stderr and produce a clean error message instead of polluting the captured path

---

## [0.1.3] — 2026-03-17

### Fixed

- `install.sh` — `BASH_SOURCE[0]: unbound variable` still occurred on bash versions where `${array[0]:-default}` triggers `set -u` when the array itself is unset (not just empty); fixed by suspending `nounset` briefly with `set +u` before reading `BASH_SOURCE`

---

## [0.1.2] — 2026-03-17

### Fixed

- `install.sh` — `BASH_SOURCE[0]: unbound variable` crash when the script is piped to bash (`curl|bash` / `wget|bash`); `BASH_SOURCE` is unset in that execution context so it now defaults to `$0`

---

## [0.1.1] — 2026-03-17

### Fixed

- `install.sh` — replaced `[[ ... ]] && main "$@"` guard with `if/fi` so the script sources cleanly under `set -euo pipefail` (the `&&` idiom exits with code 1 when the condition is false, which `set -e` treated as fatal)
- `install.sh` — `latest_version` no longer exits non-zero when the GitHub API returns no `tag_name` (e.g. curl failure or no releases yet)

---

## [0.1.0] — 2026-03-17

Initial release.

### Added

**Core**
- Single static Go binary built with Cobra
- XDG-compliant config (`~/.config/lerd/`) and data (`~/.local/share/lerd/`) directories
- Global config at `~/.config/lerd/config.yaml` with sensible defaults
- Per-project `.lerd.yaml` override support
- Linux distro detection (Arch, Debian/Ubuntu, Fedora, openSUSE)
- Build metadata injected at compile time: version, commit SHA, build date

**Site management**
- `lerd park [dir]` — auto-discover and register all Laravel projects in a directory
- `lerd link [name]` — register the current directory as a named site
- `lerd unlink` — remove a site and clean up its vhost
- `lerd sites` — tabular view of all registered sites

**PHP**
- `lerd install` — one-time setup: directories, Podman network, binary downloads, DNS, nginx
- `lerd use <version>` — set the global PHP version
- `lerd isolate <version>` — pin PHP version per-project via `.php-version`
- `lerd php:list` — list installed static PHP binaries
- PHP version resolution order: `.php-version` → `.lerd.yaml` → `composer.json` → global default

**Node**
- `lerd isolate:node <version>` — pin Node version per-project via `.node-version`
- Node version resolution order: `.nvmrc` → `.node-version` → `package.json engines.node` → global default
- fnm bundled for Node version management

**TLS**
- `lerd secure [name]` — issue a locally-trusted mkcert certificate for a site
- Automatic HTTPS vhost generation
- mkcert CA installed into system trust store on `lerd install`

**Services**
- `lerd service start|stop|restart|status|list` — manage optional services
- Bundled services: MySQL 8.0, Redis 7, PostgreSQL 16, Meilisearch v1.7, MinIO

**Infrastructure**
- All containers run rootless on a dedicated `lerd` Podman network
- Nginx and PHP-FPM as Podman Quadlet containers (auto-managed by systemd)
- dnsmasq container for `.test` TLD resolution via NetworkManager
- fsnotify-based watcher daemon (`lerd-watcher.service`) for auto-discovery of new projects

**Diagnostics**
- `lerd status` — health overview: DNS, nginx, PHP-FPM containers, services, cert expiry
- `lerd dns:check` — verify `.test` resolution

**Lifecycle**
- `lerd update` — self-update from latest GitHub release (atomic binary swap)
- `lerd uninstall` — stop all containers, remove units, binary, PATH entry, optionally data
- Shell completion via `lerd completion bash|zsh|fish`

**Installer (`install.sh`)**
- curl and wget support
- Prerequisite checking with per-distro install prompts (pacman / apt / dnf / zypper)
- Automatic `lerd install` invocation post-download
- `--update`, `--uninstall`, `--check` flags
- Installs as `lerd-installer` for later use

---

[0.1.53]: https://github.com/geodro/lerd/compare/v0.1.52...v0.1.53
[0.1.52]: https://github.com/geodro/lerd/compare/v0.1.51...v0.1.52
[0.1.51]: https://github.com/geodro/lerd/compare/v0.1.50...v0.1.51
[0.1.50]: https://github.com/geodro/lerd/compare/v0.1.49...v0.1.50
[0.1.49]: https://github.com/geodro/lerd/compare/v0.1.48...v0.1.49
[0.1.48]: https://github.com/geodro/lerd/compare/v0.1.47...v0.1.48
[0.1.47]: https://github.com/geodro/lerd/compare/v0.1.46...v0.1.47
[0.1.46]: https://github.com/geodro/lerd/compare/v0.1.45...v0.1.46
[0.1.45]: https://github.com/geodro/lerd/compare/v0.1.44...v0.1.45
[0.1.44]: https://github.com/geodro/lerd/compare/v0.1.43...v0.1.44
[0.1.43]: https://github.com/geodro/lerd/compare/v0.1.42...v0.1.43
[0.1.42]: https://github.com/geodro/lerd/compare/v0.1.41...v0.1.42
[0.1.41]: https://github.com/geodro/lerd/compare/v0.1.40...v0.1.41
[0.1.40]: https://github.com/geodro/lerd/compare/v0.1.39...v0.1.40
[0.1.39]: https://github.com/geodro/lerd/compare/v0.1.38...v0.1.39
[0.1.38]: https://github.com/geodro/lerd/compare/v0.1.37...v0.1.38
[0.1.37]: https://github.com/geodro/lerd/compare/v0.1.36...v0.1.37
[0.1.36]: https://github.com/geodro/lerd/compare/v0.1.35...v0.1.36
[0.1.35]: https://github.com/geodro/lerd/compare/v0.1.34...v0.1.35
[0.1.34]: https://github.com/geodro/lerd/compare/v0.1.33...v0.1.34
[0.1.33]: https://github.com/geodro/lerd/compare/v0.1.32...v0.1.33
[0.1.32]: https://github.com/geodro/lerd/compare/v0.1.31...v0.1.32
[0.1.31]: https://github.com/geodro/lerd/compare/v0.1.30...v0.1.31
[0.1.30]: https://github.com/geodro/lerd/compare/v0.1.29...v0.1.30
[0.1.29]: https://github.com/geodro/lerd/compare/v0.1.28...v0.1.29
[0.1.28]: https://github.com/geodro/lerd/compare/v0.1.27...v0.1.28
[0.1.27]: https://github.com/geodro/lerd/compare/v0.1.26...v0.1.27
[0.1.26]: https://github.com/geodro/lerd/compare/v0.1.25...v0.1.26
[0.1.25]: https://github.com/geodro/lerd/compare/v0.1.24...v0.1.25
[0.1.24]: https://github.com/geodro/lerd/compare/v0.1.23...v0.1.24
[0.1.23]: https://github.com/geodro/lerd/compare/v0.1.22...v0.1.23
[0.1.22]: https://github.com/geodro/lerd/compare/v0.1.21...v0.1.22
[0.1.21]: https://github.com/geodro/lerd/compare/v0.1.20...v0.1.21
[0.1.20]: https://github.com/geodro/lerd/compare/v0.1.19...v0.1.20
[0.1.19]: https://github.com/geodro/lerd/compare/v0.1.18...v0.1.19
[0.1.18]: https://github.com/geodro/lerd/compare/v0.1.17...v0.1.18
[0.1.17]: https://github.com/geodro/lerd/compare/v0.1.16...v0.1.17
[0.1.16]: https://github.com/geodro/lerd/compare/v0.1.15...v0.1.16
[0.1.15]: https://github.com/geodro/lerd/compare/v0.1.14...v0.1.15
[0.1.14]: https://github.com/geodro/lerd/compare/v0.1.13...v0.1.14
[0.1.13]: https://github.com/geodro/lerd/compare/v0.1.12...v0.1.13
[0.1.12]: https://github.com/geodro/lerd/compare/v0.1.11...v0.1.12
[0.1.11]: https://github.com/geodro/lerd/compare/v0.1.10...v0.1.11
[0.1.10]: https://github.com/geodro/lerd/compare/v0.1.9...v0.1.10
[0.1.9]: https://github.com/geodro/lerd/compare/v0.1.8...v0.1.9
[0.1.8]: https://github.com/geodro/lerd/compare/v0.1.7...v0.1.8
[0.1.7]: https://github.com/geodro/lerd/compare/v0.1.6...v0.1.7
[0.1.6]: https://github.com/geodro/lerd/compare/v0.1.5...v0.1.6
[0.1.5]: https://github.com/geodro/lerd/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/geodro/lerd/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/geodro/lerd/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/geodro/lerd/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/geodro/lerd/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/geodro/lerd/releases/tag/v0.1.0
