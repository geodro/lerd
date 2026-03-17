# Changelog

All notable changes to Lerd will be documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Lerd uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

[0.1.2]: https://github.com/geodro/lerd/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/geodro/lerd/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/geodro/lerd/releases/tag/v0.1.0
