# Changelog

All notable changes to Lerd will be documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Lerd uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

### Fixed

- **`lerd php` can now run scripts outside `$HOME`** — IDEs like PhpStorm write their validation scripts to `/tmp` and call `php -d... /tmp/ide-phpinfo.php`. The container only mounts `$HOME`, so those scripts were unreachable and produced an empty output ("Failed to parse validation script output"). `runPhp` now detects any argument that is an absolute path to a host file outside `$HOME`, reads it, and streams it to the container via `stdin` / `/dev/stdin`.

---

## [1.2.2] — 2026-03-31

### Added

- **`lerd init` validates PHP version input** — the PHP version prompt now rejects invalid input such as `8,5` or plain strings; only `MAJOR.MINOR` numeric format (e.g. `8.3`) is accepted.
- **`lerd init` and `lerd env` detect services from `.env.example`** — when `.env` is absent, service detection falls back to `.env.example` so a freshly cloned project is configured correctly before `.env` is created.
- **`lerd env` waits for services to be ready before creating databases and buckets** — after starting MySQL, PostgreSQL, or RustFS, lerd now polls for readiness (`mysqladmin ping` / `pg_isready` / TCP dial) before attempting to create the database or bucket. Previously the create step could silently fail if the container had not finished initialising.
- **Automatic quadlet restoration for orphaned PHP FPM containers** — `lerd php:list` (and any command that calls `ListInstalled`) now scans `podman ps -a` for `lerd-php*-fpm` containers whose quadlet file is missing and restores it automatically, so users who lost their quadlet files do not need to reinstall PHP.

### Fixed

- **`lerd init` installs PHP FPM with a progress indicator** — when the required PHP FPM version is not yet installed, `lerd init` now shows a spinner rather than silently blocking. (PR [#34](https://github.com/geodro/lerd/pull/34))

---

## [1.2.1] — 2026-03-31

### Fixed

- **`mcp:inject` and `mcp:enable-global` fail on empty JSON config files** — `mergeMCPServersJSON` now skips `json.Unmarshal` when the target file exists but is empty, preventing a spurious "unexpected end of JSON input" error. Affects `~/.ai/mcp/mcp.json`, `~/.junie/mcp/mcp.json`, and `.mcp.json`. (PR [#31](https://github.com/geodro/lerd/pull/31))
- **`lerd new` runs `composer install` with the wrong PHP version** — `composer create-project` for Laravel now passes `--no-install --no-plugins --no-scripts` so dependency installation is deferred to `lerd setup`, where the correct PHP version is already active. (PR [#28](https://github.com/geodro/lerd/pull/28) by @voronkovich)
- **Duplicate `export PATH` entries written to `.zshrc` on repeated `lerd install`** — `appendShellRC` now checks whether the PATH line already exists before appending. (PR [#30](https://github.com/geodro/lerd/pull/30) by @voronkovich)
- **Redundant `appendShellRC` call writes a broken `export PATH=":$PATH"` line to `.zshrc`** — the call with an empty `binDir` has been removed; `ensureZshFpath` already handles the fpath setup. (PR [#29](https://github.com/geodro/lerd/pull/29) by @voronkovich)

---

## [1.2.0] — 2026-03-30

### Added

- **`lerd init`** — interactive wizard that writes PHP version, HTTPS preference, and required services to `.lerd.yaml` for project portability. On a machine with an existing `.lerd.yaml`, `lerd init` applies the saved config non-interactively, making new-machine setup a single command. `lerd setup` now runs the wizard as its first step, `lerd link` auto-secures when `secured: true` is set, and `lerd env` / `lerd isolate` / `lerd secure` all keep the file in sync.
- **`lerd console`** — run a framework's interactive console (e.g. `php artisan tinker` for Laravel, or the `console` field from the framework YAML) inside the project container. Arguments are forwarded as-is.
- **`console` MCP tool** — execute framework console commands from an AI assistant session. Resolves the correct binary via `config.GetConsoleCommand` so it works for any framework that defines a `console` field.
- **Cloudflare Tunnel backend for `lerd share`** — pass `--cloudflare` to tunnel a site via `cloudflared`. Without the flag, lerd auto-detects between ngrok and Expose as before. The tunnel is routed through the host proxy to fix Host header and TLS SNI for secured sites.
- **pcov bundled in PHP-FPM images** — pcov is now pre-installed via PECL in all lerd PHP-FPM images; `lerd php:ext add pcov` is no longer needed to run `pest --coverage`.
- **WebP support in PHP-FPM images** — gd and imagick now include WebP support out of the box (PR [#15](https://github.com/geodro/lerd/pull/15) by @ReyArlena).
- **Connection URLs and hostname note in the dashboard** — database service cards now show ready-to-use connection URLs alongside a note about the internal container hostname.

### Fixed

- **Paused site vhosts overwritten on watcher restart** — `scanWorktrees()` now skips paused sites on startup; worktree vhost generation and nginx reloads triggered by `.php-version` changes are also skipped while a site is paused (registry is still updated for when the site is unpaused).
- **`lerd console` falls back to `artisan` for Laravel** — when a Laravel project's framework YAML has no explicit `console` field, `lerd console` now correctly uses `php artisan`.

### Internal

- Unit tests for `config`, `php`, `distro`, and `envfile` packages.

---

## [1.1.2] — 2026-03-30

### Fixed

- **`lerd install` no longer hangs after "Adding shell PATH configuration"** — the interactive MCP registration prompt has been removed. Run `lerd mcp:enable-global` manually after install to register the MCP server.
- **Dashboard URL in install completion message** — now shows `http://lerd.localhost` instead of the raw `http://127.0.0.1:7073` address.

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

- **RustFS replaces MinIO** — MinIO OSS is no longer maintained; lerd now ships RustFS as its built-in S3-compatible object storage service. RustFS exposes the same API and credentials (`lerd` / `lerdpassword`) so no application changes are needed. Closes [#3](https://github.com/geodro/lerd/issues/3).
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

- **`lerd about`** — new command that prints the version, build info, project URL, and copyright.

- **CLI commands auto-start services on paused sites** — running `php artisan`, `composer`, `lerd db:export`, `lerd db:import`, or `lerd db:shell` in a paused site's directory automatically starts any services the site needs (MySQL, Redis, etc.) before executing. A notice is printed only when a service actually needs starting; if services are already running the command executes silently. The site stays paused — no vhost restore or worker restart.

- **`lerd pause` / `lerd unpause`** — pause a site without unlinking it. `lerd pause` stops all running workers (queue, schedule, reverb, stripe, and any custom workers), replaces the nginx vhost with a static landing page, and auto-stops any services no longer needed by other active sites. The paused state persists across `lerd start` / `lerd stop` cycles. `lerd unpause` restores the vhost, restarts any services the site's `.env` references, and resumes all workers that were running before the pause. The landing page includes a **Resume** button that calls the lerd API directly so you can unpause from the browser.

- **`lerd service pin` / `lerd service unpin`** — pin a service so it is never auto-stopped, even when no active sites reference it in their `.env`. Pinning immediately starts the service if it isn't already running. Unpin to restore normal auto-stop behaviour.

- **MCP `site_pause` / `site_unpause` tools** — AI agents can pause and resume sites directly, enabling workflows like "pause all sites except the one I'm working on".

- **MCP `service_pin` / `service_unpin` tools** — AI agents can pin services to keep them always available.

- **Extra ports on built-in services** — `lerd service expose <service> <host:container>` publishes an additional host port on any built-in service (mysql, redis, postgres, meilisearch, minio, mailpit). Mappings are persisted in `~/.config/lerd/config.yaml` under `services.<name>.extra_ports` and applied on every start. The service is restarted automatically if running. Use `--remove` to delete a mapping. MCP tool `service_expose` provides the same capability.

- **Reverb nginx WebSocket proxy** — when a site uses Laravel Reverb (detected via `composer.json` or `BROADCAST_CONNECTION=reverb` in `.env`), lerd now adds a `/app` location block to the nginx vhost that proxies WebSocket upgrade requests to the Reverb server running on port 8080 inside the PHP-FPM container. The block is added automatically on `lerd link` and on `reverb:start`.
- **Framework definitions** — user-defined PHP framework YAML files at `~/.config/lerd/frameworks/<name>.yaml`. Each definition describes detection rules, the document root, env file format, per-service env detection/variable injection, and background workers. `lerd framework list/add/remove` manage definitions from the CLI.
- **Framework workers** — frameworks can define named background workers (e.g. `messenger` for Symfony, `horizon` or `pulse` for Laravel) that run as systemd user services inside the PHP-FPM container. `lerd worker start <name>` / `lerd worker stop <name>` / `lerd worker list` manage them.
- **Custom workers for Laravel** — the built-in Laravel definition now has built-in `queue`, `schedule`, and `reverb` workers. Additional workers (e.g. Horizon, Pulse) can be added via `lerd framework add laravel --from-file ...`; they are merged on top of the built-in definition.
- **Generic `lerd worker` command** — `lerd worker start/stop/list` works for any framework-defined worker. `lerd queue:start`, `lerd schedule:start`, and `lerd reverb:start` are now aliases for `lerd worker start queue/schedule/reverb` and work on any framework with those workers, not just Laravel.
- **Web UI: framework worker toggles** — custom framework workers appear as indigo toggles in the Sites panel alongside queue/schedule/reverb. Each running worker shows a log tab in the site detail drawer and an indicator dot in the site list.
- **MCP `worker_start` / `worker_stop` / `worker_list`** — start, stop, or list framework-defined workers for a site via the MCP server.
- **MCP `framework_list` / `framework_add` / `framework_remove`** — manage framework definitions from an AI assistant. `framework_add` with `name: "laravel"` adds custom workers to the built-in Laravel definition.
- **MCP `sites` now includes framework and workers** — each site entry now includes its `framework` name and a `workers` array with running status per worker.
- **Docs: `Frameworks & Workers` page** — full documentation of the YAML schema, detection rules, worker definitions, and complete Symfony and WordPress examples.
- **Web UI: docs link** — a "Docs" link in the dashboard navbar opens the documentation site.

### Changed

- **`lerd service list` uses a compact two-column format** — the `Type` column has been removed. Custom services show `[custom]` inline after their status. Inactive reason and `depends on:` info now appear as indented sub-lines, keeping the output narrow on small terminals.

- **`lerd service list` / `lerd service status` shows inactive reason** — when a service is inactive, the output now includes a short note explaining why: `(no sites using this service)` for auto-stopped services, or `(start with: lerd service start <name>)` for manually stopped ones.

- **`lerd logs` accepts a site name as target** — pass a registered site name to get logs for that site's PHP-FPM container (e.g. `lerd logs my-project`). Previously only nginx, service names, and PHP version strings were accepted.

- **`lerd unlink` auto-stops unused services** — after unlinking a site, any services that were only needed by that site are automatically stopped (respecting pin and manually-started flags).

- **`db:import` and `db:export` accept a `-d`/`--database` flag** — both commands now accept an optional `--database` / `-d` flag to target a specific database. When omitted the database name falls back to `DB_DATABASE` from the project's `.env` as before. The MCP `db_export` tool gains the same optional `database` argument.

- **`lerd secure` / `lerd unsecure` restart the Stripe listener** — if a `lerd stripe:listen` service is active when HTTPS is toggled, it is automatically restarted with the updated forwarding URL so `--forward-to` stays in sync with the site's scheme.

- **MinIO: per-site bucket created by `lerd env`** — when MinIO is detected, `lerd env` now creates a bucket named after the site handle (e.g. `my_project`), sets it to public access, and writes `AWS_BUCKET=<site>` and `AWS_URL=http://localhost:9000/<site>` into `.env`. Previously `AWS_BUCKET` was hardcoded to `lerd` and `AWS_URL` had no bucket path.

- **`reverb:start` regenerates the nginx vhost** — running `lerd reverb:start` (or toggling Reverb in the web UI) now regenerates the site's nginx config and reloads nginx, ensuring the `/app` WebSocket proxy block is added to existing sites without requiring `lerd link` to be re-run.
- **`lerd env` sets correct Reverb connection values** — `REVERB_HOST`, `REVERB_PORT`, and `REVERB_SCHEME` are now derived from the site's domain and TLS state instead of hardcoded `localhost:8080`. `VITE_REVERB_*` vars are also written to match.
- **`queue_start` / `schedule_start` / `reverb_start` are no longer Laravel-only** — these CLI commands and MCP tools now work for any framework that defines a worker with that name.
- **`lerd env` respects framework env configuration** — uses the framework's configured env file, example file, format, `url_key`, and per-service detection rules instead of hardcoded Laravel paths.
- **`lerd link` / `lerd park` detect and record the framework** — the detected framework name is stored in the site registry and shown in `lerd sites`.

### Fixed

- **`lerd php` and `lerd artisan` no longer break MCP stdio transport** — both commands now allocate a TTY (`-t`) only when stdin is a real terminal. When invoked by MCP or any other pipe-based tool, the TTY flag is omitted so stdin/stdout remain clean byte streams.

- **Reverb toggle no longer appears on projects that don't use Reverb** — the UI previously showed the Reverb toggle for all Laravel sites because the built-in worker map always included `reverb`. It now gates on `cli.SiteUsesReverb()` (checks for `laravel/reverb` in composer.json or `BROADCAST_CONNECTION=reverb` in `.env`).

### Removed

- **`internal/laravel/detector.go`** — replaced by the generic `config.DetectFramework` / `config.GetFramework` system.

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
