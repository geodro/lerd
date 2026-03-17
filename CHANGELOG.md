# Changelog

All notable changes to Lerd will be documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Lerd uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
