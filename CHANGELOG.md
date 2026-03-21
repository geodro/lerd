# Changelog

All notable changes to Lerd will be documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Lerd uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [0.8.2] — 2026-03-21

### Fixed

- **413 Request Entity Too Large on file uploads** — nginx now sets `client_max_body_size 0` (unlimited) in the `http` block, applied to all vhosts. `lerd start` also rewrites `nginx.conf` on every start so future config changes take effect without running `lerd install`.
- **MCP `logs` target accepts site domains** — site names containing dots (e.g. `astrolov.com`) were incorrectly matched as PHP version strings, producing invalid container names. The PHP version check now requires the strict pattern `\d+\.\d+`.
- **MinIO `AWS_URL` set to public endpoint** — `AWS_URL` is now `http://localhost:9000` (browser-reachable) instead of `http://lerd-minio:9000` (internal container hostname). `AWS_ENDPOINT` is unchanged and remains the internal address used by PHP.
- **Services page no longer blinks** — the services list was polling every 5 seconds regardless of which tab was active, and showed a loading spinner on each poll. Polling now only runs while the services tab is visible, and the spinner only shows on the initial load.

### Added

- **DNS health watcher** — the `lerd-watcher` daemon now polls `.test` DNS resolution every 30 seconds. When resolution breaks, it waits for `lerd-dns` to be ready and re-applies the resolver configuration, replicating the repair performed by `lerd start`. Uses the configured TLD (`dns.tld` in global config, default `test`).
- **MCP `logs` target is optional** — when `target` is omitted, logs for the current site's PHP-FPM container are returned (resolved from `LERD_SITE_PATH`). Specify `target` only to view a different service or site.

### Changed

- **`make install` respects manually-stopped services** — `lerd-ui`, `lerd-watcher`, and `lerd-tray` are only restarted after install if they were already running. Services stopped via `lerd quit` are left stopped.

---

## [0.8.1] — 2026-03-21

### Fixed

- **MCP `service_start` / `service_stop` accept custom services** — the MCP tool schema previously restricted the `name` field to an enum of built-in services, causing AI assistants to refuse to call these tools for custom services added via `service_add`. The enum constraint has been removed; any registered service name is now valid.

### Changed

- **MCP SKILL and guidelines updated** — `soketi` removed from the built-in service list (dropped in v0.8.0); `service_start`/`service_stop` descriptions clarified to explicitly mention custom service support.

---

## [0.8.0] — 2026-03-21

### Added

- **`lerd reverb:start` / `reverb:stop`** — runs the Laravel Reverb WebSocket server as a persistent systemd user service (`lerd-reverb-<site>.service`), executing `php artisan reverb:start` inside the PHP-FPM container. Survives terminal sessions and restarts on failure. Also available as `lerd reverb start` / `lerd reverb stop`.
- **`lerd schedule:start` / `schedule:stop`** — runs the Laravel task scheduler as a persistent systemd user service (`lerd-schedule-<site>.service`), executing `php artisan schedule:work`. Also available as `lerd schedule start` / `lerd schedule stop`.
- **`lerd dashboard`** — opens the Lerd dashboard (`http://127.0.0.1:7073`) in the default browser via `xdg-open`.
- **Auto-configure `REVERB_*` env vars** — `lerd env` now generates `REVERB_APP_ID`, `REVERB_APP_KEY`, `REVERB_APP_SECRET`, and `REVERB_HOST`/`PORT`/`SCHEME` values when `BROADCAST_CONNECTION=reverb` is detected, using random secure values for secrets.
- **`lerd setup` runs `storage:link`** — setup now runs `php artisan storage:link` when the site's `storage/app/public` directory is not yet symlinked.
- **`lerd setup` starts the queue worker** — setup now starts `queue:start` as a final step when `QUEUE_CONNECTION=redis` is set in `.env` or `.env.example`.
- **Watcher triggers `queue:restart` on config changes** — the watcher daemon monitors `.env`, `composer.json`, `composer.lock`, and `.php-version` in every registered site and signals `php artisan queue:restart` when any of those files change (debounced). This ensures queue workers reload after deploys or PHP version changes.
- **`lerd start` / `stop` manage schedule and reverb** — `lerd start` and `lerd stop` now include all `lerd-schedule-*` and `lerd-reverb-*` service units in their start/stop sequences alongside queue workers and stripe listeners.
- **MCP tools for reverb, schedule, stripe** — new `reverb_start`, `reverb_stop`, `schedule_start`, `schedule_stop`, and `stripe_listen` tools exposed via the MCP server.
- **Web UI: schedule and reverb per-site** — the site detail panel shows whether the schedule worker and Reverb server are running, with start/stop buttons and live log streaming.
- **Web UI: `stripe:stop` action** — the dashboard now supports stopping a stripe listener from the site action menu (was start-only).
- **`WriteServiceIfChanged`** — internal helper that skips writing and running `daemon-reload` when a service unit's content is unchanged, preventing unnecessary Podman quadlet regeneration.
- **`QueueRestartForSite`** — internal function that signals a graceful queue worker restart via `php artisan queue:restart` inside the PHP-FPM container.

### Changed

- **Queue worker uses `Restart=always`** — the `lerd-queue-*` service unit now restarts unconditionally (was `Restart=on-failure`), matching the behaviour of schedule and reverb services.
- **`lerd.test` dashboard vhost removed** — `lerd install` no longer generates an nginx proxy vhost for `lerd.test`. The dashboard is only accessible at `http://127.0.0.1:7073`. The `lerd.test` domain is no longer reserved and may be used for a regular site.
- **Web UI queue/stripe start is non-blocking** — `queue:start` and `stripe:listen` site actions now run in a background goroutine so the HTTP response returns immediately rather than waiting for the service to start.

### Removed

- **Soketi service removed** — Soketi has been removed from Lerd's service list, config defaults, and env suggestions. Laravel Reverb (`lerd reverb:start`) is the recommended WebSocket solution.

---

## [0.7.0] — 2026-03-21

### Added

- **`lerd quit` command** — fully shuts down Lerd: stops all containers and services (like `lerd stop`), then also stops the `lerd-ui` and `lerd-watcher` process units, and kills the system tray.
- **Start/Stop from the web UI** — the dashboard now has Start and Stop buttons that call `lerd start` / `lerd stop` via new `/api/lerd/start`, `/api/lerd/stop`, and `/api/lerd/quit` API endpoints. The Start button is only shown when one or more core services (DNS, nginx, PHP-FPM) are not running.
- **`lerd start` resumes stripe listeners** — `lerd-stripe-*` services are now included in the start sequence alongside queue workers and the UI service.

### Changed

- **Tray quit uses `lerd quit`** — the tray's quit action now calls the new `quit` command instead of `stop`, ensuring a full shutdown including the UI and watcher processes. The menu item is renamed from "Stop Lerd & Quit" to "Quit Lerd".
- **`lerd stop` stops all services regardless of pause state** — stop now shuts down all installed services including paused ones and stripe listeners, ensuring a clean shutdown every time.

### Fixed

- **Log panel guards** — clicking to open logs for FPM, nginx, DNS, or queue services no longer attempts to open a log stream when the service is not running.

---

## [0.6.0] — 2026-03-21

### Added

- **Git worktree support** — each `git worktree` checkout automatically gets its own subdomain (`<branch>.<site>.test`) with a dedicated nginx vhost. No manual steps required.
  - The watcher daemon detects `git worktree add` / `git worktree remove` in real time via fsnotify and generates or removes vhosts accordingly. It watches `.git/` itself so it correctly re-attaches when `.git/worktrees/` is deleted (last worktree removed) and re-created (new worktree added).
  - Startup scan generates vhosts for all existing worktrees across all registered sites.
  - `EnsureWorktreeDeps` — symlinks `vendor/` and `node_modules/` from the main repo into each worktree checkout, and copies `.env` with `APP_URL` rewritten to the worktree subdomain.
  - `lerd sites` shows worktrees indented under their parent site.
  - The web UI shows worktrees in the site detail panel with clickable domain links and an open-in-browser button.
  - A git-branch icon appears on the site button in the sidebar whenever the site has active worktrees.
- **HTTPS for worktrees** — when a site is secured with `lerd secure`, all its worktrees automatically receive an SSL vhost that reuses the parent site's wildcard mkcert certificate (`*.domain.test`). No separate certificate is needed per worktree. Securing and unsecuring a site also updates `APP_URL` in each worktree's `.env`.
- **Catch-all default vhost** (`_default.conf`) — any `.test` hostname that does not match a registered site returns HTTP 444 / rejects the TLS handshake, instead of falling through to the first alphabetical vhost.
- **`stripe:listen` as a background service** — `lerd stripe:listen` now runs the Stripe CLI in a persistent systemd user service (`lerd-stripe-<site>.service`) rather than a foreground process. It survives terminal sessions and restarts on failure. `lerd stripe:listen stop` tears it down.
- **Service pause state** — `lerd service stop` now records the service as manually paused. `lerd start` and autostart on login skip paused services. `lerd stop` + `lerd start` restore the previous state: running services restart, manually stopped services stay stopped.
- **Queue worker Redis pre-flight** — `lerd queue:start` checks that `lerd-redis` is running when `QUEUE_CONNECTION=redis` is set in `.env`, and returns a friendly error with instructions rather than failing with a cryptic DNS error from PHP.

### Fixed

- **Park watcher depth** — the filesystem watcher no longer registers projects found in subdirectories of parked directories. Only direct children of a parked directory are eligible for auto-registration.
- **Nginx reload ordering for secure/unsecure** — `lerd secure` / `lerd unsecure` (and their UI/MCP equivalents) now save the updated `secured` flag to `sites.yaml` *before* reloading nginx. Previously a failed nginx reload would leave `sites.yaml` with a stale `secured` state, causing the watcher to regenerate the wrong vhost type on restart.
- **Tray always restarts on `lerd start`** — any existing tray process is killed before relaunching, preventing duplicate tray instances after repeated `lerd start` calls.
- **FPM quadlet skip-write optimisation** — `WriteFPMQuadlet` skips writing and daemon-reloading when the quadlet content is unchanged. Unnecessary daemon-reloads caused Podman's quadlet generator to regenerate all service files, which could briefly disrupt `lerd-dns` and cause `.test` resolution failures.

---

## [0.5.16] — 2026-03-20

### Fixed

- **PHP-FPM image build on restricted Podman** — fully qualify all base image names in the Containerfile (`docker.io/library/composer:latest`, `docker.io/library/php:X.Y-fpm-alpine`). Systems without unqualified-search registries configured in `/etc/containers/registries.conf` would fail with "short-name did not resolve to an alias".

---

## [0.5.15] — 2026-03-20

### Fixed

- **PHP-FPM image build on Podman** — the Containerfile now declares `FROM composer:latest AS composer-bin` as an explicit stage before copying the composer binary. Podman (unlike Docker) does not auto-pull images referenced only in `COPY --from`, causing builds to fail with "no stage or image found with that name". This also affected `lerd update` and `lerd php:rebuild` in v0.5.14, leaving containers stopped if the build failed after the old image was removed.
- **Zero-downtime PHP-FPM rebuild** — `lerd php:rebuild` no longer removes the existing image before building. The running container stays up during the build; only the final `systemctl restart` causes a brief interruption. Force rebuilds now use `--no-cache` instead of `rmi -f`.
- **UI logs panel** — clicking logs for a site whose PHP-FPM container is not running now shows a clean "container is not running" message instead of the raw podman error.
- **`lerd php` / `lerd artisan`** — running these when the PHP-FPM container is stopped now returns a friendly error with the `systemctl --user start` command instead of a raw podman error.
- **`lerd update` ensures PHP-FPM is running** — after applying infrastructure changes, `lerd update` now starts any installed PHP-FPM containers that are not running. Also fixed a cosmetic bug where "skipping rebuild" was printed even when a rebuild had just run.

---

## [0.5.14] — 2026-03-20

### Added

- **`LERD_SITE_PATH` in MCP config** — `mcp:inject` now embeds the project path as `LERD_SITE_PATH` in the injected MCP server config. The MCP server reads this at startup and uses it as the default `path` for `artisan`, `composer`, `env_setup`, `db_export`, and `site_link`, so AI assistants no longer need to pass an explicit path on every call.
- **`.ai/mcp/mcp.json` injection** — `mcp:inject` now also writes into `.ai/mcp/mcp.json` (used by Windsurf and other MCP-compatible tools), in addition to `.mcp.json` and `.junie/mcp/mcp.json`.

---

## [0.5.10] — 2026-03-20

### Fixed

- **DNS race on install/update** — `lerd install` (and by extension `lerd update`) now waits up to 15 seconds for the `lerd-dns` container to be ready before calling `ConfigureResolver()`. Previously, `resolvectl` was called immediately after the container restart, causing systemd-resolved to mark `127.0.0.1:5300` as failed and fall back to the DHCP DNS server, breaking `.test` resolution until `lerd install` was run again manually.

---

## [0.5.8] — 2026-03-20

### Fixed

- **GoReleaser archive** — split amd64 and arm64 into separate archive definitions so `lerd-tray` (amd64-only) doesn't cause a binary count mismatch error

---

## [0.5.7] — 2026-03-20

### Fixed

- **Cross-distro tray compatibility** — the main `lerd` binary is now fully static (CGO_ENABLED=0) and carries no shared library dependencies. A separate `lerd-tray` binary (built with CGO + libappindicator3) is shipped alongside it in the release tarball. At runtime `lerd tray` execs `lerd-tray`; if the helper is absent or `libappindicator3.so.1` is missing the tray is silently skipped and everything else keeps working. Fixes startup failure on Fedora and other distros where libappindicator3 is not installed by default.

---

## [0.5.6] — 2026-03-19

### Added

- **Parallel build TUI** — `lerd fetch` and `lerd php:rebuild` now build PHP-FPM images in parallel with a compact spinner UI; press Ctrl+O to toggle per-job output
- **Service image pull TUI** — `lerd service start` shows a spinner while pulling the container image if it is not already present
- **Condensed uninstall output** — `lerd uninstall` uses the same spinner UI for a cleaner experience

### Changed

- **Install output** — `lerd install` uses plain sequential output with a spinner only for the slow image pull and dnsmasq build steps; interactive sudo prompts (mkcert CA, DNS sudoers) are no longer affected by raw terminal mode
- **mkcert output indented** — output from `mkcert -install` is indented to align with the surrounding install step lines
- **Spinner timer hidden when zero** — the elapsed timer is omitted from spinner rows that complete in under one second

### Fixed

- **PHP Containerfile** — removed `pdo_sqlite` and `sqlite3` from `docker-php-ext-install`; both are bundled in the PHP Alpine base image and including them caused a `Cannot find config.m4` build error

---

## [0.5.5] — 2026-03-19

### Added

- **`lerd php:ext add/remove/list`** — manage custom PHP extensions per version; extensions are persisted in config and included in every image rebuild
- **Expanded default FPM image** — added `bz2`, `calendar`, `dba`, `ldap`, `mysqli`, `pdo_sqlite`, `sqlite3`, `soap`, `shmop`, `sysvmsg`, `sysvsem`, `sysvshm`, `xsl` (via `docker-php-ext-install`) plus `igbinary` and `mongodb` (via PECL); the default bundle now covers ~30 extensions for Herd-parity
- **Composer extension detection** — `lerd park` / `lerd link` reads `ext-*` keys from `composer.json` and warns if any required extensions are missing from the image, with an actionable hint
- **`lerd php:ini [version]`** — opens the per-version user php.ini in `$EDITOR`; the file is mounted into the FPM container at `/usr/local/etc/php/conf.d/98-lerd-user.ini` and created automatically with commented examples on first use

---

## [0.5.4] — 2026-03-19

### Added

- **Custom services**: users can now define arbitrary OCI-based services without recompiling. Config lives at `~/.config/lerd/services/<name>.yaml`.
  - `lerd service add [file.yaml]` — add from a YAML file or inline flags (`--name`, `--image`, `--port`, `--env`, `--env-var`, `--data-dir`, `--detect-key`, `--detect-prefix`, `--init-exec`, `--init-container`, `--dashboard`, `--description`)
  - `lerd service remove <name>` — stop (if running), remove quadlet and config; data directory preserved
  - `lerd service list` — shows built-in and custom services with a `[custom]` type column
  - `lerd service start/stop` — works for custom services
  - `lerd start` / `lerd stop` — includes installed custom services
  - `lerd env` — auto-detects custom services via `env_detect`, applies `env_vars`, runs `site_init.exec`
  - `lerd status` — includes custom services in the `[Services]` section
  - Web UI services tab — shows custom services with start/stop and dashboard link
  - System tray — shows custom services (slot pool expanded from 7 to 20)
- **`{{site}}` / `{{site_testing}}` placeholders** in `env_vars` and `site_init.exec` — substituted with the project site handle at `lerd env` time
- **`site_init`** YAML block — runs a `sh -c` command inside the service container once per project when `lerd env` detects the service (for DB/collection creation, user setup, etc.)
- **`dashboard`** field on custom services and built-in service responses — shows an "Open" button in the web UI when the service is active; dashboard URLs for built-ins (Mailpit, MinIO, Meilisearch) moved from hardcoded JS to the API response
- **README simplified** — now a slim landing page pointing to the docs site; full documentation at `geodro.github.io/lerd`
- **Docs updated** — `docs/usage/services.md` extended with full custom services reference

### Fixed

- Custom service data directory is now created automatically before starting (`podman` refused to mount a non-existent host path)
- `lerd service remove` now checks unit status before stopping — skips stop if not running, and aborts removal if stop fails (prevents orphaned running containers)

---

## [0.5.3] — 2026-03-19

### Fixed

- **Tray not restarting after `lerd update`**: `lerd install` was killing the tray with `pkill` but only relaunching it when `lerd-tray.service` was enabled. If the tray was started directly (`lerd tray`), it was killed and never restarted. Now tracks whether the tray was running before the kill and relaunches it directly when systemd is not managing it.

---

## [0.5.2] — 2026-03-19

### Fixed

- `lerd db:create` and `lerd db:shell` were missing from the binary — `cmd/lerd/main.go` was not staged in the v0.5.1 commit

---

## [0.5.1] — 2026-03-19

### Added

- **`lerd db:create [name]`** / **`lerd db create [name]`**: creates a database and a `<name>_testing` database in one command. Name resolution: explicit argument → `DB_DATABASE` from `.env` → project name (site registry or directory). Reports "already exists" instead of failing when a database is present. Available for both MySQL and PostgreSQL.
- **`lerd db:shell`** / **`lerd db shell`**: opens an interactive MySQL (`mysql -uroot -plerd`) or PostgreSQL (`psql -U postgres`) shell inside the service container, connecting to the project's database automatically. Replaces the need to run `podman exec --tty lerd-mysql mysql …` manually.

### Changed

- **`lerd env` now creates a `<name>_testing` database** alongside the main project database when setting up MySQL or PostgreSQL. Both databases report "already exists" if they were previously created.

---

## [0.5.0] — 2026-03-19

### Added

- **System tray applet** (`lerd tray`): a desktop tray icon for KDE, GNOME (with AppIndicator extension), waybar, and other SNI-compatible environments. The applet detaches from the terminal automatically and polls `http://127.0.0.1:7073` every 5 seconds. Menu includes:
  - 🟢/🔴 overall running status with per-component nginx and DNS indicators
  - **Open Dashboard** — opens the web UI
  - **Start / Stop Lerd** toggle
  - **Services section** — lists all active services with 🟢/🔴 status; clicking a service starts or stops it
  - **PHP section** — lists all installed PHP versions; current global default is marked ✔; clicking switches the global default via `lerd use`
  - **Autostart at login** toggle — enables or disables `lerd-autostart.service`
  - **Check for update** — polls GitHub; if a newer version is found the item changes to "⬆ Update to vX.Y.Z" and clicking opens a terminal with a confirmation prompt before running `lerd update`
  - **Stop Lerd & Quit** — runs `lerd stop` then exits the tray
- **`--mono` flag** for `lerd tray`: defaults to `true` (white monochrome icon); pass `--mono=false` for the red colour icon
- **`lerd autostart tray enable/disable`**: registers/removes `lerd-tray.service` as a user systemd unit that starts the tray on graphical login
- **`lerd start` starts the tray**: if `lerd-tray.service` is enabled it is started via systemd; otherwise, if no tray process is already running, `lerd tray` is launched directly
- **`make build-nogui`**: headless build (`CGO_ENABLED=0 -tags nogui`) for CI or servers; `lerd tray` returns a clear error instead of failing to link

### Changed

- **Build now requires CGO and `libappindicator3`** (`libappindicator-gtk3` on Arch, `libappindicator3-dev` on Debian/Ubuntu, `libappindicator-gtk3-devel` on Fedora). The `make build` target sets `CGO_ENABLED=1 -tags legacy_appindicator` automatically.
- **`lerd-autostart.service`** now declares `After=graphical-session.target` so the tray (which needs a display) is available when `lerd start` runs at login.
- **Web UI update flow**: the "Update" button has been removed. When an update is available the UI now shows `vX.Y.Z available — run lerd update in a terminal`. The `/api/update` endpoint has been removed. This avoids silent failures caused by `sudo` steps in `lerd install` that require a TTY.
- **`/api/status`** now includes a `php_default` field with the global default PHP version, used by the tray to mark the active version with ✔.

---

## [0.4.3] — 2026-03-19

### Fixed

- **DNS broken after install on Fedora (and other NM + systemd-resolved systems)**: the NetworkManager dispatcher script and `ConfigureResolver()` were calling `resolvectl domain $IFACE ~test`, which caused systemd-resolved to mark the interface as `Default Route: no`. This meant queries for anything outside `.test` (i.e. all internet DNS) had no route and were refused. Fixed by also passing `~.` as a routing domain in both places — the interface now handles `.test` specifically via lerd's dnsmasq and remains the default route for all other queries.
- **`.test` DNS fails after reboot/restart**: `lerd start` was calling `resolvectl dns` to point systemd-resolved at lerd-dns (port 5300) immediately after the container unit became active — but dnsmasq inside the container wasn't ready to accept connections yet. systemd-resolved would try port 5300, fail, mark it as a bad server, and fall back to the upstream DNS for the rest of the session. Fixed by waiting up to 10 seconds for port 5300 to accept TCP connections before calling `ConfigureResolver()`.
- **Clicking a site URL after disabling HTTPS still opened the HTTPS version**: the nginx HTTP→HTTPS redirect was a `301` (permanent), which browsers cache indefinitely. After disabling HTTPS, the browser would serve the cached redirect instead of hitting the server. Changed to `302` (temporary) so browsers always check the server, and disabling HTTPS takes effect immediately.

---

## [0.4.2] — 2026-03-19

### Changed

- **`lerd setup` detects the correct asset build command from `package.json`**: instead of always suggesting `npm run build`, the setup step now reads `scripts` from `package.json` and picks the first available candidate in priority order: `build` (Vite / default), `production` (Laravel Mix), `prod`. The step label reflects the detected command (e.g. `npm run production`). If none of the candidates exist, the build step is omitted from the selector.

---

## [0.4.1] — 2026-03-19

### Fixed

- **`lerd status` TLS certificate check**: `certExpiry` was passing raw PEM bytes directly to `x509.ParseCertificate`, which expects DER-encoded bytes. The fix decodes the PEM block first, so certificate expiry is read correctly and sites no longer show "cannot read cert" when the cert file exists and is valid.

---

## [0.4.0] — 2026-03-19

### Added

- **Xdebug toggle** (`lerd xdebug on/off [version]`): enables or disables Xdebug per PHP version by rebuilding the FPM image with Xdebug installed and configured (`mode=debug`, `start_with_request=yes`, `client_host=host.containers.internal`, port 9003). The FPM container is restarted automatically. `lerd xdebug status` shows enabled/disabled for all installed versions.
- **`lerd fetch [version...]`**: pre-builds PHP FPM images for the specified versions (or all supported: 8.1–8.5) so the first `lerd use <version>` is instant. Skips versions whose images already exist.
- **`lerd db:import <file.sql>`** / **`lerd db:export [-o file]`**: import or export a SQL dump using the project's `.env` DB settings. Supports MySQL/MariaDB (`lerd-mysql`) and PostgreSQL (`lerd-postgres`). Also available as `lerd db import` / `lerd db export`.
- **`lerd share [site]`**: exposes the current site publicly via ngrok or Expose. Auto-detects which tunnel tool is installed; use `--ngrok` or `--expose` to force one. Forwards to the local nginx port with the correct `Host` header so nginx routes to the right vhost.
- **`lerd setup`**: interactive project bootstrap command — presents a checkbox list of steps (composer install, npm ci, lerd env, lerd mcp:inject, php artisan migrate, php artisan db:seed, npm run build, lerd secure, lerd open) with smart defaults based on project state. `lerd link` always runs first (mandatory, not in the list) to ensure the site is registered with the correct PHP version before any subsequent step. `--all` / `-a` runs everything without prompting (CI-friendly); `--skip-open` skips opening the browser.

### Fixed

- **PHP version detection order**: `composer.json` `require.php` now takes priority over `.php-version`, so projects declaring `"php": "^8.4"` in `composer.json` automatically use PHP 8.4 even if a stale `.php-version` file says otherwise. Explicit `.lerd.yaml` overrides still take top priority.
- **`lerd link` preserves HTTPS**: re-linking a site that was already secured now regenerates the SSL vhost (not an HTTP vhost), so `https://` continues to work after a re-link.
- **`lerd link` preserves `secured` flag**: re-linking no longer resets a secured site to `secured: false`.
- **`lerd secure` / `lerd unsecure` directory name resolution**: sites in directories with real TLDs (e.g. `astrolov.com`) are now resolved correctly by path lookup, so the commands no longer error with "site not found" when the directory name differs from the registered site name.

---

## [0.3.0] — 2026-03-18

### Added

- `lerd env` command: copies `.env.example` → `.env` if missing, detects which services the project uses, applies lerd connection values, starts required services, generates `APP_KEY` if missing, and sets `APP_URL` to the registered `.test` domain
- `lerd unsecure [name]` command: removes the mkcert TLS cert and reverts the site to HTTP
- `lerd secure` and `lerd unsecure` now automatically update `APP_URL` in the project's `.env` to `https://` or `http://` respectively
- `lerd install` now installs a `/etc/sudoers.d/lerd` rule granting passwordless `resolvectl dns/domain/revert` — required for the autostart service which cannot prompt for a sudo password
- PHP FPM images now include the `gmp` extension
- **MCP server** (`lerd mcp`): JSON-RPC 2.0 stdio server exposing lerd as a Model Context Protocol tool provider for AI assistants (Claude Code, JetBrains Junie, and any MCP-compatible client). Tools: `artisan`, `sites`, `service_start`, `service_stop`, `queue_start`, `queue_stop`, `logs`
- **`lerd mcp:inject`**: writes `.mcp.json`, `.claude/skills/lerd/SKILL.md`, and `.junie/mcp/mcp.json` into a project directory. Merges into existing `mcpServers` configs — other servers (e.g. `laravel-boost`, `herd`) are preserved unchanged
- **UI: queue worker toggle** in the Sites tab — amber toggle to start/stop the queue worker per site; spinner while toggling; error text on failure; **logs** link opens the live log drawer for that worker when running
- **UI: Unlink button** in the Sites tab — small red-bordered button that confirms, calls `POST /api/sites/{domain}/unlink`, and removes the site from the table client-side immediately
- **`lerd unlink` parked-site behaviour**: unlinking a site under a parked directory now marks it as `ignored` in the registry instead of removing it, preventing the watcher from re-registering it on next scan. Running `lerd link` in the same directory clears the flag. Non-parked sites are still removed from the registry entirely
- `GET /api/sites` filters out ignored sites so they are invisible in the UI
- `queue:start` and `queue:stop` are now also available as API actions via `POST /api/sites/{domain}/queue:start` and `POST /api/sites/{domain}/queue:stop`, enabling UI and MCP control

### Fixed

- DNS `.test` routing now works correctly after autostart: `resolvectl revert` is called before re-applying per-interface DNS settings so systemd-resolved resets the current server to `127.0.0.1:5300`; previously, resolved would mark lerd-dns as failed during boot (before it started) then fall back to the upstream DNS for all queries including `.test`, causing NXDOMAIN on every `.test` lookup
- `fnm install` no longer prints noise to the terminal when a Node version is already installed

### Changed

- `lerd start` and `lerd stop` now start/stop containers in parallel — startup is noticeably faster on multi-container setups
- `lerd start` now re-applies DNS resolver config on every invocation, ensuring `.test` routing is always correct after reboot or network changes
- `lerd park` now skips already-registered sites instead of overwriting them, preserving settings such as TLS status and custom PHP version
- `lerd install` completion message now shows both `http://lerd.test` and `http://127.0.0.1:7073` as fallback
- Composer is now stored as `composer.phar`; the `composer` shim runs it via `lerd php`
- Autostart service now declares `After=network-online.target` and runs at elevated priority (`Nice=-10`)

---

## [0.2.0] — 2026-03-17

### Changed

- UI completely redesigned: dark theme inspired by Laravel.com with near-black background, red accents, and top navbar replacing the sidebar
- Light / Auto / Dark theme toggle added to the navbar; preference persists in localStorage

---

## [0.1.66] — 2026-03-17

### Fixed

- `lerd start` now detects missing PHP FPM images (e.g. after `podman rmi`) and automatically rebuilds them before starting units
- `lerd status` now reports `image missing` with a `lerd php:rebuild <version>` hint instead of just showing the container as not running

---

## [0.1.65] — 2026-03-17

### Fixed

- PHP 8.5 FPM image now builds successfully: `opcache` is already compiled into PHP 8.5 so `docker-php-ext-enable opcache` is now a no-op (`|| true`); `apk update` is run before `apk add` to avoid stale index warnings; `redis` falls back to building from GitHub source when PECL fails

---

## [0.1.64] — 2026-03-17

### Fixed

- `redis` and `imagick` PHP extensions now fall back to building from GitHub source when the PECL stable release doesn't compile against the current PHP API version (e.g. PHP 8.5) — redis is required so the build fails if both methods fail; imagick remains optional

---

## [0.1.63] — 2026-03-17

### Fixed

- `pecl install redis` is now also non-fatal during PHP FPM image builds — the `redis` extension (like `imagick`) doesn't yet compile against PHP 8.5's new API; both extensions are best-effort and the build succeeds regardless

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

[0.6.0]: https://github.com/geodro/lerd/compare/v0.5.16...v0.6.0
[0.5.3]: https://github.com/geodro/lerd/compare/v0.5.2...v0.5.3
[0.5.2]: https://github.com/geodro/lerd/compare/v0.5.1...v0.5.2
[0.5.1]: https://github.com/geodro/lerd/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/geodro/lerd/compare/v0.4.3...v0.5.0
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
