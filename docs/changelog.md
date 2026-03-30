# Changelog

All notable changes to Lerd will be documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Lerd uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [1.1.1] — 2026-03-30

### Added

- **CI badge on README** — the README now shows a live CI status badge linked to the `ci.yml` workflow.

### Fixed

- **MCP registration prompt unresponsive when installing via pipe** — `lerd install` reads the "Register lerd MCP globally?" prompt answer from `/dev/tty` instead of stdin. When the installer is run via a pipe (`curl ... | sh`), stdin is the pipe and `fmt.Scan` returns immediately with no input; opening `/dev/tty` directly reads from the actual terminal regardless of how the process was started.

### Internal

- **Release workflow now gates on CI** — the `release.yml` workflow runs build, test, vet, and format checks before invoking GoReleaser. A tag push on a broken commit will now fail before any artifacts are published.

---

## [1.1.0] — 2026-03-30

### Added

- **`lerd new <name-or-path>`** — scaffold a new PHP project using the framework's `create` command. Defaults to Laravel (`composer create-project laravel/laravel`). Pass `--framework=<name>` to use any framework that defines a `create` field. Extra args can be forwarded to the scaffold command after `--`. The `project_new` MCP tool provides the same functionality for AI assistants.
- **`create` field in framework definitions** — framework YAML files now support a `create` property (e.g. `create: composer create-project symfony/skeleton`). The target directory is appended automatically by `lerd new`. The `--create` flag was also added to `lerd framework add`.
- **`project_new` MCP tool** — scaffold a new project from an AI assistant session. Accepts `path` (required), `framework` (default: `laravel`), and `args` (extra scaffold flags). Follow with `site_link` and `env_setup` to register and configure the new site.
- **`lerd mcp:enable-global`** — registers the lerd MCP server at Claude Code user scope (and Windsurf / JetBrains Junie global configs) so lerd tools are available in every AI session without per-project configuration. During `lerd install`, if Claude Code is detected and lerd is not yet registered, the installer prompts to run this automatically.
- **`site_php` MCP tool** — change the PHP version for a registered site from your AI assistant. Writes `.php-version`, updates the site registry, regenerates the nginx vhost, and reloads nginx in one call. The target FPM container must be running.
- **`site_node` MCP tool** — change the Node.js version for a registered site. Writes `.node-version` and installs the version via fnm if not already present.
- **CWD fallback for MCP path resolution** — the MCP server now falls back to the working directory Claude was opened in when `LERD_SITE_PATH` is not set. This means `path` can be omitted from `artisan`, `composer`, `env_setup`, `site_link`, `db_export`, and other tools when running in a global MCP session — just open Claude in the project directory.

### Fixed

- **`lerd setup` npm step fails without a lockfile** — the npm install step now runs `npm ci` when `package-lock.json` or `yarn.lock` is present, and falls back to `npm install` otherwise. Previously `npm ci` was always used, causing the step to fail on projects without a lockfile. (PR [#5](https://github.com/geodro/lerd/pull/5) by @voronkovich)
- **Duplicate `PATH` entry on `lerd install`** — `add_to_path` in `install.sh` now checks the live `$PATH` before modifying shell rc files. If the install directory is already present, the function returns early and skips rc modification. (PR [#7](https://github.com/geodro/lerd/pull/7) by @voronkovich)
- **zsh completions moved to XDG directory** — zsh completions are written to `~/.local/share/zsh/site-functions/_lerd` instead of `~/.zfunc/_lerd`, aligning with the XDG base directory convention. (PR [#8](https://github.com/geodro/lerd/pull/8) by @voronkovich)
- **`.php-version` changes not reflected in nginx** — writing a `.php-version` file (via `lerd isolate` or directly) updated the queue worker but left the nginx vhost pointing at the old FPM socket. The watcher daemon now detects when the resolved PHP version changes, updates the site registry, regenerates the vhost, and reloads nginx automatically (debounced to 2 seconds).
- **PHP version resolution order** — `.php-version` now takes priority over `composer.json`'s `require.php` constraint, matching the documented and intuitive precedence (explicit pin beats inferred constraint).

---

## [1.0.4] — 2026-03-26

### Fixed

- **`.test` domains unavailable from PHP-FPM containers** — v1.0.3 fixed internet access by setting real upstream DNS servers (e.g. `192.168.0.x`) on the `lerd` Podman network, but this caused aardvark-dns to skip systemd-resolved, breaking `.test` resolution from inside containers. `lerd start` and `lerd install` now use pasta's built-in DNS proxy at `169.254.1.1` (read from the rootless-netns `info.json`) as the aardvark-dns upstream. This address chains through systemd-resolved, which routes `.test` queries to lerd-dns and forwards all other queries to real upstream servers — giving containers both `.test` resolution and full internet access.
- **HTTPS to `.test` sites fails from inside PHP-FPM containers (`cURL error 60`)** — PHP code making outbound HTTPS requests to local `.test` domains (e.g. Reverb broadcasting, internal API calls) received SSL certificate errors because the mkcert root CA was not trusted inside the container. The PHP-FPM image build now copies the mkcert root CA into the Alpine trust store (`update-ca-certificates`), so all `.test` HTTPS certificates are trusted. Existing images are automatically rebuilt on `lerd update`.
- **Reverb / queue / schedule workers not restarted after `php:rebuild`** — when `php:rebuild` replaced and restarted the PHP-FPM containers, workers running inside those containers via `podman exec` (Reverb, queue, schedule) were killed by the `BindsTo` systemd dependency but not brought back up automatically. `php:rebuild` now explicitly restarts all such workers after the containers are back online.

---

## [1.0.3] — 2026-03-26

### Fixed

- **No internet access from PHP-FPM containers** — on systems where `/etc/resolv.conf` points to a stub resolver (`127.0.0.53` via systemd-resolved), aardvark-dns could not forward external DNS queries because the stub address is only reachable on the host's loopback, not from inside the container network namespace. `lerd start` and `lerd install` now detect the real upstream DNS servers (reading `/run/systemd/resolve/resolv.conf` first) and set them on the `lerd` Podman network so aardvark-dns forwards correctly.

---

## [1.0.2] — 2026-03-25

### Added

- **RustFS replaces MinIO** — MinIO OSS is no longer maintained; lerd now ships RustFS as its built-in S3-compatible object storage service. RustFS exposes the same API and credentials (`lerd` / `lerdpassword`) so no application changes are needed.
- **`lerd minio:migrate`** — one-command migration from an existing MinIO installation to RustFS. Stops the MinIO container, copies data to the RustFS data directory, removes the MinIO quadlet, updates `config.yaml`, and starts RustFS. The original MinIO data directory is preserved for manual cleanup.
- **Auto-migration prompt during `lerd update`** — if a MinIO data directory is detected at update time, lerd offers to run the migration automatically before continuing.
- **`lerd.localhost` custom domain** — the Lerd dashboard is now accessible at `http://lerd.localhost` (nginx proxies the domain to the UI service). `lerd dashboard` opens the new URL. `.localhost` resolves to `127.0.0.1` natively on all modern systems with no DNS configuration.
- **Installable PWA** — the dashboard ships a web app manifest (`/manifest.webmanifest`) and SVG icons so it can be installed as a standalone app from Chrome or other PWA-capable browsers.

### Fixed

- **502 Bad Gateway on Inertia.js full-page refreshes** — nginx vhost templates now include `fastcgi_buffers 16 16k` and `fastcgi_buffer_size 32k`, preventing `upstream sent too big header` errors caused by large FastCGI response headers (common on routes with heavy session/flash data).

---

## [1.0.1] — 2026-03-25

### Added

- **`lerd shell`** — opens an interactive `sh` session inside the project's PHP-FPM container. The PHP version is resolved the same way as every other lerd command (`.php-version`, `composer.json`, global default). The working directory is set to the site root. If the site is paused, any services referenced in `.env` are started automatically before the shell opens.
- **Shell completions auto-installed on `lerd install`** — fish completions are written to `~/.config/fish/completions/lerd.fish`; zsh completions to `~/.zfunc/_lerd` with the required `fpath` and `compinit` lines appended to `.zshrc`; bash completions to `~/.local/share/bash-completion/completions/lerd`.
- **Pause/unpause propagates to git worktrees** — when a site is paused, all its worktree checkouts also receive a paused nginx vhost with a **Resume** button. The button targets the parent site so clicking it unpauses both the parent and all worktrees at once. Unpausing restores all worktree vhosts and removes the paused HTML files.

### Fixed

- **`lerd park` refuses to park a framework project root** — if the target directory is itself a Laravel/framework project, lerd now prints a helpful message and suggests `lerd link` instead of silently misbehaving.
- **`lerd park` no longer registers framework subdirectories as sites** — when a project root is accidentally used as a park directory, subdirectories like `app/`, `vendor/`, and `public/` are now skipped with a warning rather than being registered as phantom sites.

---

## [1.0.0] — 2026-03-25

### Added

- **Laravel Horizon support** — lerd auto-detects `laravel/horizon` in `composer.json` and provides dedicated `lerd horizon:start` / `lerd horizon:stop` commands that run `php artisan horizon` as a persistent systemd user service (`lerd-horizon-{site}`). When Horizon is detected, the **Queue** toggle in the web UI is replaced by a **Horizon** toggle, and a **Horizon** log tab appears in the site detail panel while Horizon is running. Pause/unpause correctly stops and resumes the Horizon service alongside other workers. MCP tools `horizon_start` and `horizon_stop` provide the same control to AI assistants.

- **Service dependencies (`depends_on`)** — custom services can now declare which services they depend on. Starting a service with dependencies starts those dependencies first; starting a dependency automatically starts any services that depend on it; stopping a dependency cascade-stops its dependents first. Declare via the `depends_on` YAML field, the `--depends-on` flag on `lerd service add`, or the `depends_on` parameter in the `service_add` MCP tool.

- **`lerd man` — terminal documentation browser** — browse and search the built-in docs without leaving the terminal. Opens an interactive TUI with arrow-key navigation, live filtering by title or content, and a scrollable markdown pager. Pass a page name to jump directly (e.g. `lerd man sites`). Set `GLAMOUR_STYLE=light` to override the default dark theme. Works in non-TTY mode too: `lerd man | cat` prints a table of contents and `lerd man sites | cat` prints raw markdown.
- **`lerd about`** — prints the version, build info, project URL, and copyright.
- **CLI commands auto-start services on paused sites** — running `php artisan`, `composer`, `lerd db:export`, `lerd db:import`, or `lerd db:shell` in a paused site's directory automatically starts any services the site needs (MySQL, Redis, etc.) before executing. A notice is printed only when a service actually needs starting; if services are already running the command executes silently. The site stays paused — no vhost restore or worker restart.

- **`lerd pause` / `lerd unpause`** — pause a site without unlinking it. `lerd pause` stops all running workers (queue, schedule, reverb, stripe, and any custom workers), replaces the nginx vhost with a static landing page, and auto-stops any services no longer needed by other active sites. The paused state persists across `lerd start` / `lerd stop` cycles. `lerd unpause` restores the vhost, restarts any services the site's `.env` references, and resumes all workers that were running before the pause. The landing page includes a **Resume** button that calls the lerd API directly so you can unpause from the browser.
- **`lerd service pin` / `lerd service unpin`** — pin a service so it is never auto-stopped, even when no active sites reference it in their `.env`. Pinning immediately starts the service if it isn't already running. Unpin to restore normal auto-stop behaviour.
- **Extra ports on built-in services** — `lerd service expose <service> <host:container>` publishes an additional host port on any built-in service (mysql, redis, postgres, meilisearch, minio, mailpit). Mappings are persisted in `~/.config/lerd/config.yaml` under `services.<name>.extra_ports` and applied on every start. The service is restarted automatically if running. Use `--remove` to delete a mapping.
- **Reverb nginx WebSocket proxy** — when a site uses Laravel Reverb (detected via `composer.json` or `BROADCAST_CONNECTION=reverb` in `.env`), lerd adds a `/app` location block to the nginx vhost that proxies WebSocket upgrade requests to the Reverb server running on port 8080 inside the PHP-FPM container. The block is added automatically on `lerd link` and on `reverb:start`.
- **Framework definitions** — user-defined PHP framework YAML files at `~/.config/lerd/frameworks/<name>.yaml`. Each definition describes detection rules, the document root, env file format, per-service env detection/variable injection, and background workers. `lerd framework list/add/remove` manage definitions from the CLI.
- **Framework workers** — frameworks can define named background workers (e.g. `messenger` for Symfony, `horizon` or `pulse` for Laravel) that run as systemd user services inside the PHP-FPM container. `lerd worker start <name>` / `lerd worker stop <name>` / `lerd worker list` manage them.
- **Custom workers for Laravel** — the built-in Laravel definition includes `queue`, `schedule`, and `reverb` workers. Additional workers (e.g. Horizon, Pulse) can be added via `lerd framework add laravel`.
- **Generic `lerd worker` command** — `lerd worker start/stop/list` works for any framework-defined worker. `lerd queue:start`, `lerd schedule:start`, and `lerd reverb:start` are now aliases for `lerd worker start queue/schedule/reverb` and work on any framework with those workers, not just Laravel.
- **Web UI: framework worker toggles** — custom framework workers appear as indigo toggles in the Sites panel alongside queue/schedule/reverb. Each running worker shows a log tab in the site detail drawer and an indicator dot in the site list.
- **Web UI: docs link** — a "Docs" link in the dashboard navbar opens the documentation site.
- **MCP `worker_start` / `worker_stop` / `worker_list`** — start, stop, or list framework-defined workers for a site via the MCP server.
- **MCP `framework_list` / `framework_add` / `framework_remove`** — manage framework definitions from an AI assistant. `framework_add` with `name: "laravel"` adds custom workers to the built-in Laravel definition.
- **MCP `service_expose`** — publish or remove an extra host port on a built-in service via the MCP server.
- **MCP `site_pause` / `site_unpause` tools** — AI agents can pause and resume sites directly.
- **MCP `service_pin` / `service_unpin` tools** — AI agents can pin services to keep them always available.
- **MCP `sites` now includes framework and workers** — each site entry includes its `framework` name and a `workers` array with running status per worker.

### Changed

- **`lerd service list` uses a compact two-column format** — the `Type` column has been removed. Custom services show `[custom]` inline after their status. Inactive reason and `depends on:` info now appear as indented sub-lines, keeping the output narrow on small terminals.

- **`lerd service list` / `lerd service status` shows inactive reason** — when a service is inactive, the output now includes a short note: `(no sites using this service)` for auto-stopped services, or `(start with: lerd service start <name>)` for manually stopped ones.
- **`lerd logs` accepts a site name as target** — pass a registered site name to get logs for that site's PHP-FPM container (e.g. `lerd logs my-project`). Previously only nginx, service names, and PHP version strings were accepted.
- **`lerd unlink` auto-stops unused services** — after unlinking a site, any services that were only needed by that site are automatically stopped (respecting pin and manually-started flags).
- **`db:import` and `db:export` accept a `-d`/`--database` flag** — both commands now accept an optional `--database` / `-d` flag to target a specific database. When omitted the database name falls back to `DB_DATABASE` from the project's `.env`. The MCP `db_export` tool gains the same optional `database` argument.
- **`lerd secure` / `lerd unsecure` restart the Stripe listener** — if a `lerd stripe:listen` service is active when HTTPS is toggled, it is automatically restarted with the updated forwarding URL so `--forward-to` stays in sync with the site's scheme.
- **MinIO: per-site bucket created by `lerd env`** — when MinIO is detected, `lerd env` creates a bucket named after the site handle (e.g. `my_project`), sets it to public access, and writes `AWS_BUCKET=<site>` and `AWS_URL=http://localhost:9000/<site>` into `.env`.
- **`reverb:start` regenerates the nginx vhost** — running `lerd reverb:start` (or toggling Reverb in the web UI) now regenerates the site's nginx config and reloads nginx, ensuring the `/app` WebSocket proxy block is added to existing sites without requiring `lerd link` to be re-run.
- **`lerd env` sets correct Reverb connection values** — `REVERB_HOST`, `REVERB_PORT`, and `REVERB_SCHEME` are now derived from the site's domain and TLS state instead of hardcoded `localhost:8080`. `VITE_REVERB_*` vars are also written to match.
- **`queue_start` / `schedule_start` / `reverb_start` are no longer Laravel-only** — these CLI commands and MCP tools now work for any framework that defines a worker with that name.
- **`lerd env` respects framework env configuration** — uses the framework's configured env file, example file, format, `url_key`, and per-service detection rules instead of hardcoded Laravel paths.
- **`lerd link` / `lerd park` detect and record the framework** — the detected framework name is stored in the site registry and shown in `lerd sites`.

### Fixed

- **`lerd php` and `lerd artisan` no longer break MCP stdio transport** — both commands now allocate a TTY (`-t`) only when stdin is a real terminal. When invoked by MCP or any other pipe-based tool, the TTY flag is omitted so stdin/stdout remain clean byte streams.
- **Reverb toggle no longer appears on projects that don't use Reverb** — the UI previously showed the Reverb toggle for all Laravel sites. It now gates on whether `laravel/reverb` is in `composer.json` or `BROADCAST_CONNECTION=reverb` is in `.env`.

---

## [0.9.1] — 2026-03-22

### Added

- **MCP `service_env` tool** — returns the recommended Laravel `.env` connection variables for any service (built-in or custom) as a key/value map. Agents can call `service_env(name: "mysql")` to inspect connection settings without running `env_setup` or modifying `.env`. Works for all six built-in services and any custom service registered via `service_add`.

### Changed

- **`lerd update` does a fresh version check** — bypasses the 24-hour update cache and always fetches the latest release tag from GitHub directly. After a successful update the cache is refreshed so `lerd status` and `lerd doctor` stop showing a stale "update available" notice.
- **`lerd update` ignores git-describe suffixes** — dev/dirty builds (e.g. `v0.9.0-dirty`) are now treated as equal to the corresponding release when comparing versions, so locally-built binaries no longer trigger a spurious update prompt.

---

## [0.9.0] — 2026-03-22

### Added

- **`lerd doctor` command** — full environment diagnostic. Checks podman, systemd user session, linger, quadlet/data dir writability, config validity, DNS resolution, port 80/443/5300 conflicts, PHP-FPM image presence, and update availability. Reports OK/FAIL/WARN per check with a hint for every failure and a summary line at the end.
- **`lerd status` shows watcher and update notice** — `lerd-watcher` is now included in the status output alongside DNS, nginx, and PHP-FPM. A highlighted banner is printed when a newer version is cached.
- **Background update checker** — checks GitHub for a new release once per 24 hours; result is cached to `~/.local/share/lerd/update-check.json`. Fetches relevant CHANGELOG sections between the current and latest version. Used by `lerd status`, `lerd doctor`, the web UI, and the system tray.
- **MCP `status` tool** — returns structured JSON with DNS (ok + tld), nginx (running), PHP-FPM per version (running), and watcher (running). Recommended first call when a site isn't loading.
- **MCP `doctor` tool** — runs the full `lerd doctor` diagnostic and returns the text report. Use when the user reports setup issues or unexpected behaviour.
- **Watcher structured logging** — the watcher package now uses `slog` throughout. Set `LERD_DEBUG=1` in the environment to enable debug-level output at runtime; watcher is otherwise silent except for WARN/ERROR events.
- **Web UI: Watcher card** — the System tab now shows whether `lerd-watcher` is running. When stopped, a **Start** button appears to restart it without opening a terminal. The card also streams live watcher logs (DNS repair events, fsnotify errors, worktree timeouts) directly in the browser.
- **Web UI: grouped worker accordions** — queue workers, schedule workers, Stripe listeners, and Reverb servers are now grouped into collapsible accordions on the Services tab. Click a group header to expand it; only one group is open at a time. Mobile pill navigation is split into core services + group toggle pills with expandable sub-rows.
- **Tray: update badge** — the "Check for update..." menu item shows "⬆ Update to vX.Y.Z" when a new version is cached. Per-site workers (queue, schedule, Stripe, Reverb) are no longer listed in the tray services section.

### Changed

- **`lerd update` shows changelog and asks for confirmation** — before downloading anything, `lerd update` now fetches and prints the CHANGELOG sections for every version between the current and latest release, then prompts `Update to vX.Y.Z? [y/N]`. The update only proceeds on an explicit `y`/`yes`; pressing Enter or anything else cancels.

### Fixed

- **`lerd start` now starts `lerd-watcher`** — the watcher service was missing from the start sequence and could only be stopped by `lerd quit`, never started. `lerd start` now includes it alongside `lerd-ui`.

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
- **Watcher triggers `queue:restart` on config changes** — the watcher daemon monitors `.env`, `composer.json`, `composer.lock`, and `.php-version` in every registered site and signals `php artisan queue:restart` when any of those files change (debounced).
- **`lerd start` / `stop` manage schedule and reverb** — `lerd start` and `lerd stop` now include all `lerd-schedule-*` and `lerd-reverb-*` service units in their start/stop sequences alongside queue workers and stripe listeners.
- **MCP tools for reverb, schedule, stripe** — new `reverb_start`, `reverb_stop`, `schedule_start`, `schedule_stop`, and `stripe_listen` tools exposed via the MCP server.
- **Web UI: schedule and reverb per-site** — the site detail panel shows whether the schedule worker and Reverb server are running, with start/stop buttons and live log streaming.
- **Web UI: `stripe:stop` action** — the dashboard now supports stopping a stripe listener from the site action menu (was start-only).

### Changed

- **Queue worker uses `Restart=always`** — the `lerd-queue-*` service unit now restarts unconditionally (was `Restart=on-failure`).
- **`lerd.test` dashboard vhost removed** — `lerd install` no longer generates an nginx proxy vhost for `lerd.test`. The dashboard is only accessible at `http://127.0.0.1:7073`.
- **Web UI queue/stripe start is non-blocking** — `queue:start` and `stripe:listen` site actions now run in a background goroutine.

### Removed

- **Soketi service removed** — Soketi has been removed from Lerd's service list. Laravel Reverb (`lerd reverb:start`) is the recommended WebSocket solution.

---

## [0.7.0] — 2026-03-21

### Added

- **`lerd quit` command** — fully shuts down Lerd: stops all containers and services (like `lerd stop`), then also stops the `lerd-ui` and `lerd-watcher` process units, and kills the system tray.
- **Start/Stop from the web UI** — the dashboard now has Start and Stop buttons via new `/api/lerd/start`, `/api/lerd/stop`, and `/api/lerd/quit` API endpoints.
- **`lerd start` resumes stripe listeners** — `lerd-stripe-*` services are now included in the start sequence alongside queue workers and the UI service.

### Changed

- **Tray quit uses `lerd quit`** — the tray's quit action now calls the new `quit` command, ensuring a full shutdown including the UI and watcher processes.
- **`lerd stop` stops all services regardless of pause state** — stop now shuts down all installed services including paused ones and stripe listeners.

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

## [0.5.13] — 2026-03-20

### Fixed

- **`lerd mcp:inject`** — `db_export` tool now correctly appears in the generated SKILL.md and `.junie/guidelines.md` (skill content was omitted from the v0.5.12 release)

---

## [0.5.12] — 2026-03-20

### Added

- **MCP: `db_export`** — export the project database to a SQL dump file (defaults to `<database>.sql` in the project root); reads connection details from `.env`

### Fixed

- **`lerd artisan` / `lerd php` / `lerd node` / `lerd npm` / `lerd npx`** — lerd usage/help text and "Error: exit status N" no longer appear when the subprocess exits with a non-zero code (e.g. failed tests); only the subprocess output is shown and the original exit code is propagated to the shell

---

## [0.5.11] — 2026-03-20

### Added

- **MCP: 14 new tools** — the `lerd mcp` server now exposes the full project lifecycle:
  - `composer` — run Composer inside the PHP-FPM container
  - `node_install` / `node_uninstall` — install or uninstall Node.js versions via fnm
  - `runtime_versions` — list installed PHP and Node.js versions with defaults
  - `env_setup` — configure `.env` for lerd (detects services, starts them, creates DB, generates `APP_KEY`, sets `APP_URL`)
  - `site_link` / `site_unlink` — register or unregister a directory as a lerd site
  - `secure` / `unsecure` — enable or disable HTTPS for a site; updates `APP_URL` automatically
  - `xdebug_on` / `xdebug_off` / `xdebug_status` — toggle Xdebug per PHP version and check state
  - `service_add` / `service_remove` — register or deregister custom OCI services
- **MCP: `service_start` / `service_stop` support custom services** — previously only worked for built-in services
- **MCP: `.junie/guidelines.md`** — `lerd mcp:inject` now writes a lerd context section into Junie's guidelines file (merged, not overwritten) so JetBrains Junie has the same tool knowledge as Claude Code
- **Web UI: tab persistence** — active tab (Sites, Services, System) is now stored in the URL hash (`/#services`) so refreshing the browser returns to the same tab

### Fixed

- MCP skill content updated with all new tools, workflows, and architecture notes

---

## [0.5.9] — 2026-03-20

### Added

- **`lerd node:install <version>`** — install a Node.js version globally via fnm
- **`lerd node:uninstall <version>`** — uninstall a Node.js version via fnm
- **Node.js card in System tab** — lists all installed Node versions with an inline install form; replaces the install form that was previously in the Services tab
- **`lerd php:rebuild` now restarts containers** — automatically restarts all FPM containers after rebuilding images instead of printing manual instructions

### Fixed

- **`lerd tray` not opening after update** — `install.sh --update` was not copying the `lerd-tray` helper binary alongside `lerd`
- **`laravel new` and other PHP CLI tools now work end-to-end** — the PHP-FPM container image now includes Composer and Node.js/npm so subprocesses spawned by PHP (e.g. `composer create-project`, `npm install`) resolve correctly inside the container
- **`composer` and `laravel` global tools found inside container** — `lerd php` now passes the correct `HOME` and `COMPOSER_HOME` env vars and includes the Composer global bin dir in PATH so globally installed tools like the Laravel installer are found
- **Node/npm/npx shims work inside containers** — shims now use `fnm` directly (statically linked, works in Alpine) instead of calling `lerd` (glibc binary, incompatible with Alpine musl)
- **Shims use absolute paths** — `php`, `composer`, `node`, `npm`, `npx` shims now reference their binaries by absolute path, eliminating PATH-dependent failures in subprocess contexts

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
- **`dashboard`** field — shows an "Open" button in the web UI when the service is active; dashboard URLs for built-ins (Mailpit, MinIO, Meilisearch) moved from hardcoded JS to the API response
- **README simplified** — now a slim landing page pointing to the docs site
- **Docs updated** — `docs/usage/services.md` extended with full custom services reference

### Fixed

- Custom service data directory is now created automatically before starting
- `lerd service remove` now checks unit status before stopping — skips stop if not running, and aborts removal if stop fails

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

---

[0.6.0]: https://github.com/geodro/lerd/compare/v0.5.16...v0.6.0
[0.5.16]: https://github.com/geodro/lerd/compare/v0.5.15...v0.5.16
[0.5.15]: https://github.com/geodro/lerd/compare/v0.5.14...v0.5.15
[0.5.14]: https://github.com/geodro/lerd/compare/v0.5.13...v0.5.14
[0.5.13]: https://github.com/geodro/lerd/compare/v0.5.12...v0.5.13
[0.5.12]: https://github.com/geodro/lerd/compare/v0.5.11...v0.5.12
[0.5.11]: https://github.com/geodro/lerd/compare/v0.5.9...v0.5.11
[0.5.9]: https://github.com/geodro/lerd/compare/v0.5.4...v0.5.9
[0.5.4]: https://github.com/geodro/lerd/compare/v0.5.3...v0.5.4
[0.5.3]: https://github.com/geodro/lerd/compare/v0.5.2...v0.5.3
[0.5.2]: https://github.com/geodro/lerd/compare/v0.5.1...v0.5.2
[0.5.1]: https://github.com/geodro/lerd/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/geodro/lerd/compare/v0.4.3...v0.5.0
[0.4.3]: https://github.com/geodro/lerd/compare/v0.4.2...v0.4.3
[0.4.2]: https://github.com/geodro/lerd/compare/v0.4.1...v0.4.2
[0.4.1]: https://github.com/geodro/lerd/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/geodro/lerd/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/geodro/lerd/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/geodro/lerd/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/geodro/lerd/releases/tag/v0.1.0
