---
v-pre: true
---

# Changelog

All notable changes to Lerd will be documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Lerd uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [1.10.0] — 2026-04-10

### Added

- **Framework definition store** — community framework store backed by `geodro/lerd-frameworks` with `lerd framework search`, `lerd framework install`, and `lerd framework update` commands. Definitions auto-fetch when linking a project and auto-refresh after 24 hours. MCP tools `framework_search` and `framework_install` expose the store to AI assistants. (#103)
- **Framework-agnostic worker system** — all hardcoded Laravel worker logic replaced with a generic system driven by framework YAML definitions. Dedicated commands (`queue`, `schedule`, `reverb`, `horizon`) are now aliases that read from the framework definition. Workers support `conflicts_with`, proxy config with auto port assignment, and port collision prevention across sites.
- **Worker add/remove CLI and MCP tools** — `lerd worker add` and `lerd worker remove` manage custom workers in `.lerd.yaml` (project-level) or the global framework overlay (`--global`). Orphaned workers (running units with no framework definition) are detected and surfaced in `worker list`, `worker stop`, and setup.
- **PHP version ranges** — framework definitions declare supported PHP min/max ranges. `lerd link` and `lerd init` clamp the PHP version to the framework's supported range. `lerd sites` and the UI show the framework version (e.g. "Laravel 11").
- **`{{domain}}` and `{{scheme}}` template vars** — framework env var templates can reference the site's primary domain and TLS scheme. `.env` keys like `APP_URL`, `VITE_REVERB_HOST`, and `VITE_REVERB_SCHEME` sync automatically when the primary domain changes.
- **Selenium service preset** — bundled `selenium` preset (selenium/standalone-chromium) for browser testing with Laravel Dusk. Auto-detected via `composer_detect` on `laravel/dusk`, patches `DuskTestCase.php`, and includes noVNC on port 7900 for watching tests live. New `share_hosts` field on custom services maps `.test` domains to the nginx container IP.
- **Cursor MCP support** — `mcp:inject` and `mcp:enable-global` now write Cursor configuration (`.cursor/mcp.json` and `.cursor/rules/lerd.mdc`). (#132)
- **Ghostscript in PHP-FPM** — `ghostscript` added to the base PHP-FPM image for PDF manipulation with libraries like Spatie MediaLibrary. (#138)
- **mysql-client in PHP-FPM** — `mysql-client` added to the PHP-FPM image so `mysqldump` works inside `lerd php` sessions. (#142)

### Changed

- **MCP tool responses optimised for AI agents** — ANSI escape codes stripped from all CLI output. `doctor`, `check`, and `env_check` return structured JSON instead of raw text. `env:check` no longer exits non-zero.
- **CI auto-rebuilds PHP images** — a scheduled workflow checks Docker Hub daily for upstream `php:X.Y-fpm-alpine` security patches and triggers a force rebuild when new digests appear.

### Fixed

- **`php:rebuild` reused stale base images** — `lerd php rebuild` now always pulls fresh base images instead of building on top of potentially outdated cached layers. (#140)
- **`npm run build` failed when `node_modules` missing** — build step is now guarded so it skips gracefully when dependencies haven't been installed. (#133)

---

## [1.9.4] — 2026-04-10

### Fixed

- **Extra volume mounts lost after install/update** — `lerd install` rewrote nginx and service quadlets from raw templates, dropping extra volume mounts for projects outside `$HOME`. Mounts now survive install and update cycles.

---

## [1.9.3] — 2026-04-10

### Fixed

- **Projects outside `$HOME` failed with "chdir: No such file or directory"** — the PHP-FPM and nginx containers only bind-mount `$HOME`, so projects in `/var/www`, `/opt/projects`, or similar paths could not be served or exec'd into. Lerd now automatically injects extra volume mounts into both containers when it detects a project outside the home directory. Mounts are added transparently during `lerd link`, `lerd park`, or any exec command (`lerd php`, `composer`, `laravel new`) and cleaned up on `lerd unlink` / `lerd unpark`. (#120)
- **Env file keys appended instead of uncommented** — when a `.env` key existed but was commented out (`#DB_HOST=...`), `lerd env` appended a duplicate instead of uncommenting the existing line in place.

### Added

- **`lerd doctor` checks for crun** — warns when `crun` is not installed, since it is the recommended OCI runtime for rootless Podman.

---

## [1.9.2] — 2026-04-10

### Fixed

- **Site service badges missed .env-detected services** — badges on the site detail panel only showed services declared in `.lerd.yaml`. Now also scans the site's `.env` for `lerd-{name}` references (both built-in and custom services), matching the same auto-detection logic the Services tab already uses.

---

## [1.9.1] — 2026-04-09

### Fixed

- **Queue workers silently lost on uninstall+reinstall** — `queueStartExplicit` ran a Redis preflight that returned an error before the unit file was written. Install-time `restoreSiteInfrastructure` runs *before* any services are started, so for sites with `QUEUE_CONNECTION=redis` the write step always failed and the worker units stayed missing on disk while systemd remembered them as `not-found failed`. The preflight is gone; the dependency now lives in the systemd unit itself. `lerd-queue-<site>.service` declares `After=`/`Wants=` for whatever the queue backend needs (`lerd-redis.service` when `QUEUE_CONNECTION=redis`, `lerd-mysql.service` / `lerd-postgres.service` for database-backed queues) on top of the FPM container, and `lerd-horizon-<site>.service` always declares `lerd-redis.service`. systemd handles the activation order and `Restart=always` covers the small ready-window between activation and the backing container accepting connections.
- **Preset-installed services not regenerated on reinstall** — `restoreSiteInfrastructure` only handled inline custom services and built-in named refs. Preset references like `mariadb-11` (declared in `.lerd.yaml` as `mariadb-11: {preset: mariadb, version: "11"}`) fell through to `ensureServiceQuadlet`, which only knows about built-ins, so the silently-swallowed `unknown service` error left sites with no quadlet for any preset-installed service after an uninstall+reinstall cycle. The restore path now goes through `ProjectService.Resolve()` which already knows how to render both inline and preset references back into a concrete `CustomService`.

### Changed

- **`lerd status` shows `[preset]` for preset-installed services** instead of grouping them under `[custom]`. Hand-rolled custom services keep the `[custom]` label.
- **Tagline reworded** — `lerd --help`, the `install.sh` banner, and the goreleaser GitHub release notes header now read `Lerd — Podman-powered local PHP dev environment for Linux` instead of `Laravel Herd for Linux — …`.
- **Services walkthrough** (`docs/getting-started/services.md`) updated to lead with the bundled preset flow for MongoDB, phpMyAdmin, and pgAdmin (`lerd service preset <name>`) instead of the hand-rolled YAML each one used to require. Adminer, Elasticsearch, and RabbitMQ stay as full YAML recipes since there's no preset for them yet. Adminer's port bumped to 8083 to avoid colliding with the `mongo-express` preset on 8082.

---

## [1.9.0] — 2026-04-09

### Added

- **Service presets** — opt-in bundled service definitions surfaced via `lerd service preset` (list / install) and a `+` picker on the Web UI's Services tab. First batch ships `phpmyadmin`, `pgadmin`, `mongo`, `mongo-express`, and `stripe-mock` as embedded YAML that becomes a normal custom service once installed, so every existing `lerd service` subcommand (start/stop/remove/expose/pin) keeps working unchanged. Installed presets are filtered out of the picker; after install the user lands on the new service detail panel and the service auto-starts.
- **Multi-version preset families** — presets can declare multiple versions in a single YAML (e.g. `mysql` 8.0/8.4/9.0, `mariadb` 10.11/11.4) and `lerd service preset` shows version pills on `list`, prompts for a version on install, and persists the chosen tag in `.lerd.yaml`. Family discovery groups versions by base name in both the CLI list and the Web UI picker.
- **Preset MCP tools** — `service_preset_list` and `service_preset_install` expose the preset catalog and install flow to AI assistants, sharing the install path with the CLI through `serviceops.InstallPresetByName`. Re-run `lerd mcp:inject` in existing projects to pick up the new tool descriptions.
- **Custom service `files:` field** — declare inline-rendered config files materialised on the host and bind-mounted into the container, with optional `mode` (octal perms) and `chown: true` (adds `:U` so podman re-chowns to the container's non-root uid). Used by the `pgadmin` preset to ship a `servers.json` + `pgpass` that autoconnects to `lerd-postgres`. Files re-render on every `lerd service start` so editing the YAML and restarting picks up changes.
- **Custom service `connection_url:` field** — non-built-in databases now get the same "Open connection URL" link surface as the built-in mysql/postgres services. The detail panel renders a real `<a>` element pointing at `mysql://`, `postgresql://`, or `mongodb://` so right-click "Copy link" works and left-click hands the URL to the user's registered DB client (DBeaver, TablePlus, Compass, etc.).
- **Recursive `service start`** — `lerd service start <svc>` now ensures every entry in `depends_on` is up first, recursively, in both the CLI and the Web UI. Pairs with the existing recursive stop that takes dependents down before the parent. Starting any preset that depends on a built-in (`phpmyadmin`, `pgadmin`) auto-starts the database.
- **Preset dependency gating at install time** — installing a preset whose dependency is another *custom* service (e.g. `mongo-express` on `mongo`) is rejected with a clear error until the dependency is installed first. Built-in deps (mysql, postgres) are auto-satisfied. The Web UI's Add button is disabled with a matching amber "install mongo first" hint.
- **Database service quality-of-life suggestions** — the detail panel of every database service (mysql, postgres, and an installed `mongo`) now shows a sky-blue suggestion banner offering to install its paired admin UI when missing. The banner is dismissable per-preset and the dismissal persists in `localStorage`. When the admin UI is installed, the header gains an Open phpMyAdmin / pgAdmin / Mongo Express button that auto-starts the admin service if needed.
- **Lerd health dot in the Web UI** — the Lerd entry in the System list now reflects overall core health (green when DNS / nginx / watcher are all running, red when any is down, yellow when an update is available) instead of only the update flag. The lerd logo in the left rail gains a small yellow badge when an update is available and is clickable, jumping straight to the Lerd entry.
- **One-click update terminal** — when an update is available, the Lerd entry exposes an "Open terminal & update" button that POSTs to the new loopback-only `/api/lerd/update-terminal` endpoint, which spawns the user's preferred terminal emulator (kitty / foot / alacritty / wezterm / ghostty / konsole / gnome-terminal / xfce4-terminal / tilix / terminator / xterm) running `lerd update` so the host can prompt for sudo and stream download progress.
- **Getting-started walkthroughs** — new `docs/getting-started/laravel.md`, `symfony.md`, `wordpress.md`, and `services.md` pages plus a `docs/usage/lifecycle.md` reference covering how Lerd's units come up at boot and how `start` / `stop` / `autostart` interact.

### Changed

- **`autostart` is now a single coherent switch** — `cfg.Autostart.Disabled` is the canonical source of truth for whether lerd comes up at login. Toggling it enables/disables every `lerd-*.container` quadlet (by adding/stripping the `[Install]` section so the podman generator stops emitting the `default.target.wants` symlink) and every `lerd-*.service` unit (UI, watcher, per-site worker/queue/schedule/horizon/reverb/stripe) together. Toggling does not stop or start anything currently running — the user is in the middle of working and a session-level switch should not yank infrastructure out from under them. Use `lerd start` / `lerd stop` for live state.
- **`lerd autostart tray` removed** — the tray is now governed by the same single autostart switch as everything else. The standalone `autostart tray` subcommand and the `lerd-autostart.service` unit file are gone.
- **Service display labels** — the Web UI now shows phpMyAdmin, pgAdmin, MySQL, PostgreSQL, Meilisearch, Mailpit, RustFS, MongoDB, Mongo Express, and Stripe Mock with their proper casing.

### Fixed

- **Tray autostart was broken** — the tray autostart path went through the now-removed `lerd-autostart.service` shim and stopped enabling on fresh installs. The unified autostart toggle now covers the tray too, the per-unit autostart toggle is wired up correctly, and `lerd install` honours the persisted autostart state.

---

## [1.8.0] — 2026-04-09

### Added

- **`lerd lan:expose` / `lan:unexpose` / `lan:status`** — unified switch to share a lerd dev environment with another machine on the local network. Off by default; every container port now binds `127.0.0.1` (was `0.0.0.0` since v0.1.0), so untrusted wifi is safe out of the box. Service containers (mysql, postgres, redis, meilisearch, rustfs, mailpit) stay loopback-only even when LAN exposure is on; only nginx flips to `0.0.0.0`, since Laravel apps in `lerd-php-fpm` reach services through the podman bridge regardless of host bind. Quadlets are rewritten centrally via `podman.WriteQuadletDiff` so flipping the switch only restarts units whose on-disk content actually changed.
- **Remote dashboard access** — the dashboard at port 7073 is gated by two independent flags: `cfg.LAN.Exposed` is the top-level kill switch and `cfg.UI.PasswordHash` adds HTTP Basic auth on top. LAN clients only reach the dashboard when both are set; loopback always bypasses both. Stale credentials cannot survive `lan:unexpose`. The dashboard's "Remote dashboard access" card distinguishes active / inert / disabled states so the user sees when credentials are stored but blocked by `lan:unexpose`. UI feedback during a toggle streams NDJSON progress events from `POST /api/lan/status`; the card polls every 5s while on the System tab so CLI toggles are reflected without a page reload.
- **`http://lerd.localhost` as a usable bookmark** — `lerd-nginx` serves the static dashboard HTML, icons, and PWA manifest from the `lerd.localhost` vhost, with `/api/*` explicitly returning 444 so a LAN curl forging the Host header cannot reach `lerd-ui` through the proxy. The dashboard JS detects when it was loaded from `lerd.localhost` and rewrites all fetch, EventSource, and favicon img srcs to absolute `http://localhost:7073` URLs so they hit `lerd-ui` directly over loopback.
- **`lerd remote-setup`** — generates a one-shot 15-minute code and prints a curl one-liner the remote machine runs to install mkcert, trust the lerd root CA, and configure its resolver (NetworkManager+dnsmasq, systemd-resolved 254+, standalone dnsmasq, or macOS `/etc/resolver`). The endpoint is gated by token presence + RFC 1918 source IP + brute-force lockout. The bootstrap script's epilogue warns that the server IP is hardcoded into the resolver dropin and explains how to re-bootstrap if the server moves networks.
- **`app_url` field in `.lerd.yaml` and `sites.yaml`** — new precedence chain for `APP_URL`: `.lerd.yaml` `app_url` (committed, shared across machines) > `sites.yaml` `app_url` (per-machine override) > the default `<scheme>://<primary-domain>` generator. `lerd setup` no longer overwrites a custom `APP_URL` on every run — set it once in `.lerd.yaml` and lerd respects it. The `.lerd.yaml` `app_url` is silently suppressed when its host points at a domain that the conflict filter dropped, so `.env` never ends up writing a hostname owned by another site.
- **Soft-fallback domain conflict handling** — when `lerd link` or the parked-directory watcher tries to register a domain another site already owns, the conflicting domain is now filtered out (instead of failing the whole link) and a clear WARN line is printed naming the owning site. Surviving domains still register; if every domain conflicts, lerd falls back to a freshly generated `<dirname>.<tld>` with a numeric suffix. `.lerd.yaml` is never modified on disk — the original `domains:` list stays so the conflict is visible to the UI and self-heals on the next link if the owning site is removed.
- **Domain conflict UI surface** — the site detail header's "+N more" pill now counts conflicted domains and shows an amber warning icon when present (hover reveals each conflicted entry with the owning site name). The Manage Domains modal renders conflicted entries at the top with a warning icon, the domain struck-through, a "used by &lt;site&gt;" pill, and a small trash button that removes the entry from `.lerd.yaml` only (no registry, vhost, or cert touched). The `domain:remove` server action detects conflict-filtered entries and routes them to a `.lerd.yaml`-only delete path.
- **`[Remote Access]` section in `lerd status`** — new block showing LAN exposure state and dashboard remote-access state, with hints when off. Refactored into a testable `printRemoteAccessStatus` helper.
- **Tray "Expose to LAN" toggle** — new menu item that shells out to `lerd lan expose / unexpose`, mirroring the autostart toggle.
- **Dynamic colour tray icon** — white L when lerd is running, red L when stopped. The default flag flipped from `--mono=true` to `--mono=false` so the colour icon is what users see by default; mono mode is still available for OS-recoloured template icons. The icon's dark background was stripped so it's transparent on the panel.

### Changed

- **Tray "Open Dashboard" opens `http://lerd.localhost`** instead of the bare `127.0.0.1:7073` loopback URL. Tray API polling stays on loopback so the tray works before nginx is up.
- **Tray paused services render with a yellow dot** instead of red, so user-initiated stops are visually distinct from broken services.
- **`lerd doctor` "linger enabled" check renamed** to "systemd linger" so the WARN row no longer reads as if linger is in fact enabled.

### Fixed

- **`lerd uninstall` left the tray running** — the uninstall flow stopped and disabled all systemd units but never killed standalone tray processes (launched from the desktop file or `lerd tray`). The tray kept running after the binary was gone, with no way to dismiss it short of `pkill`. Uninstall now calls the existing `killTray` helper after the unit teardown.
- **`lerd install` hang when installing the Laravel installer** — the installer prompted for the Laravel installer on every run and then shelled out through the composer shim, which routes through `lerd php` and depends on cwd-based PHP detection. When the install command runs from `$HOME` with no project metadata, detection fell back to `cfg.PHP.DefaultVersion` and handed composer to a possibly-missing container. Worse, `composer global require` triggers symfony/flex / plugin trust prompts which sat invisibly inside `podman exec -t -i`, making the whole step look stuck with no output. Fixed by skipping the prompt entirely when no PHP version is installed, and when it does run, bypassing the shim — picking a known-installed PHP (preferring the configured default), ensuring its FPM container is running, and `podman exec`'ing `composer global require --no-interaction laravel/installer` directly.

---

## [1.7.1] — 2026-04-08

### Added

- **Database picker in `lerd init`** — the wizard's services step is now split into a single-choice **Database** select (sqlite / mysql / postgres) and a multi-select for everything else. The default is seeded from any database already in `.lerd.yaml`, then `DB_CONNECTION` in `.env` (or `.env.example` for fresh clones), falling back to SQLite. After the wizard completes, `lerd env` runs automatically so the choice immediately lands in `.env` — picking MySQL/PostgreSQL writes the connection vars and creates the project database (plus `_testing`), picking SQLite writes `DB_CONNECTION=sqlite` and creates `database/database.sqlite` if it's missing.
- **Runtime database prompt in `lerd env`** — when run interactively on a Laravel project whose `.env` says `DB_CONNECTION=sqlite` and whose `.lerd.yaml` doesn't yet pick a database, `lerd env` now prompts for a deliberate choice (Keep SQLite / MySQL / PostgreSQL) and persists it so subsequent runs don't re-ask. Skipped automatically when stdin isn't a TTY (CI, MCP, scripted runs) and for frameworks with explicit env service rules (Symfony, WordPress, etc.) that don't use `DB_CONNECTION`.
- **`db_set` MCP tool** — pick the database for a Laravel project from an AI assistant: `db_set(database: "sqlite" | "mysql" | "postgres")`. Persists the choice to `.lerd.yaml` (replacing any prior database — the choice is exclusive), rewrites the `DB_` keys in `.env`, starts the service if needed, and creates the database (or the SQLite file). The companion `env_setup` tool's description now points at `db_set` so AI assistants know to call it before `env_setup` on fresh Laravel clones — `env_setup` alone leaves `DB_CONNECTION=sqlite` untouched.
- **SQLite as a first-class env-time choice** — `serviceEnvVars["sqlite"]` now applies `DB_CONNECTION=sqlite` and `DB_DATABASE=database/database.sqlite`. The `lerd env` flow special-cases sqlite so it isn't treated as a podman service: no quadlet, no `service_start`, just the env vars and the file creation. The user's database choice in `.lerd.yaml` is authoritative — switching from mysql → sqlite (or vice versa) skips the auto-detection of the previous database in `.env`.

### Fixed

- **`vendor_bins` / `vendor_run` missing from injected MCP skills** — the new vendor/bin tooling shipped in v1.7.0 was registered with the MCP server but absent from the skill content that `lerd mcp:inject` writes into `.claude/skills/lerd/SKILL.md` and `.junie/guidelines.md`, so AI assistants weren't told the tools existed. Both files now describe the tools with examples for pest, phpunit, pint, phpstan, and rector. Re-run `lerd mcp:inject` in existing projects to pick up the updated skill content.

---

## [1.7.0] — 2026-04-08

### Added

- **Application log viewer in the UI** — site detail view now has an App Logs tab that parses application log files into a structured table with level, date, and message columns, expandable to show full stacktraces. Frameworks declare log file locations and parser format via a new `logs` field in their YAML; Laravel defaults to `storage/logs/*.log` with Monolog parsing. Auto-selects the site with the most recent log activity on page load, refreshes every 5 seconds, and supports search filtering plus a Latest/All toggle. Entries display oldest-first (newest at the bottom), pinned to the bottom on every refresh, matching the streaming container/queue/worker log panes.
- **`vendor/bin` shortcuts and `lerd test` / `lerd a` aliases** — any composer-installed binary in the project's `vendor/bin` is now callable directly as `lerd <name>` (e.g. `lerd pest`, `lerd pint`, `lerd phpstan`), routed through the project's PHP-FPM container with `vendor/bin` prepended to `PATH`. Built-in lerd commands always win on name collisions. Two new shortcuts: `lerd a` (alias for `artisan`) and `lerd test` (shortcut for `artisan test`). The same surface is exposed to MCP clients via `vendor_bins` (list) and `vendor_run` (execute). Closes #101.
- **Laravel installer shipped globally** — `lerd install` now offers to install `laravel/installer` as a global composer package and creates a `laravel` shim in `BinDir` routed through `lerd php`, so the `laravel` command works directly in the terminal the way Herd ships it. The prompt defaults to yes and runs before the parallel TUI to avoid stdin conflicts. Closes #98.
- **Site favicons in the UI** — the UI detects `favicon.ico`/`svg`/`png` in each site's public directory and serves them via `GET /api/sites/{domain}/favicon`. The sites list and detail header now display the favicon when available, falling back to the status dot.

### Changed

- **PHP and Node version selects deferred until loaded** — the version dropdowns in the site detail view now show static placeholders while the version lists are still loading, preventing the browser from resetting `selectedSite.php_version` / `node_version` to an empty string and causing spurious change events.

### Fixed

- **Dark mode dropdown readability** — the PHP and Node version selectors now apply explicit option background and text colors so the dropdown menu is readable in dark mode.

---

## [1.6.3] — 2026-04-06

### Changed

- **Tray switched to libayatana-appindicator** — the system tray now uses the actively maintained ayatana fork instead of the legacy libappindicator3. No behavior change; ayatana is the default backend in getlantern/systray and is already present on Ubuntu desktops.
- **`lerd update` defaults to yes** — pressing Enter now confirms the update instead of cancelling.

### Fixed

- **DNS broken on systems without NetworkManager** — the resolved drop-in file was written with 0600 permissions (unreadable by systemd-resolved), breaking `.test` domain resolution on omarchy and similar systems. Fixed by setting correct permissions (0644) via `sudoWriteFile`.
- **Sudoers missing resolved paths** — extended the sudoers drop-in to cover systemd-resolved config paths for passwordless install/start on resolved-only systems.

---

## [1.6.2] — 2026-04-06

### Fixed

- **MissingAppKeyException on fresh project** — `lerd env` now generates `APP_KEY` directly in `.env` when `vendor/` does not exist yet, instead of failing silently on `artisan key:generate`. This prevents Laravel's `MissingAppKeyException` during `composer install` post-install scripts in the `lerd new` → `lerd link` → `lerd setup` flow.
- **`composer install` using wrong PHP version in setup** — `lerd setup` now runs `composer install` inside the project's PHP-FPM container, matching the `composer.json` PHP constraint. Previously it used the host composer shim which could resolve to the global default PHP version.
- **PHP version detection from `composer.json` ignores installed versions** — the constraint resolver now picks the highest installed PHP version satisfying the `composer.json` `require.php` constraint (e.g. `^8.3` with 8.3 and 8.4 installed → 8.4). Supports `^`, `~`, `>=`, `<`, `||`, `*`, and AND constraints. Falls back to the literal minimum when no installed version matches.

---

## [1.6.1] — 2026-04-06

### Fixed

- **Fresh install missing default PHP-FPM** — `lerd install` now always builds and starts the default PHP version, even with no registered sites. Previously `lerd new` would fail on a fresh install because no PHP-FPM container existed.
- **Install not restoring services** — `lerd install` now restores service quadlets (mysql, redis, custom services) from `.lerd.yaml`, pulls missing images, and starts them. Workers no longer fail on reinstall because their dependencies are running.
- **Install not restoring workers** — `lerd install` now calls `restoreSiteInfrastructure` to recreate worker units from `.lerd.yaml` after services are started.
- **FPM not restored for sites using default PHP** — both `lerd install` and `lerd start` now fall back to the configured default PHP version when a site has no explicit `PHPVersion`, instead of skipping it.
- **UI stripe toggle not syncing `.lerd.yaml`** — toggling the Stripe listener from the web UI now writes the workers list to `.lerd.yaml`, matching the behaviour of all other worker toggles.
- **Uninstall spinner with no expandable output** — replaced the StepRunner spinner (Ctrl+O did nothing) with the same `step()`/`ok()` output style used by install.

---

## [1.6.0] — 2026-04-06

### Added

- **Framework setup commands** — framework definitions now support a `setup` field with one-off bootstrap commands (migrations, storage links, fixtures) shown in `lerd setup`. Laravel's hardcoded storage:link/migrate/db:seed steps are now part of the built-in framework definition. Custom frameworks define their own via YAML.
- **Conditional checks on workers and setup commands** — both `workers` and `setup` entries support an optional `check` field (`file` or `composer`) to conditionally show them based on project dependencies (e.g. messenger worker only shown when `symfony/messenger` is installed).
- **Service version placeholders** — framework env vars support `{{mysql_version}}`, `{{postgres_version}}`, `{{redis_version}}`, and `{{meilisearch_version}}` placeholders, resolved from the running service image tag at `lerd env` time.
- **`--setup` flag for `lerd framework add`** — define setup commands via CLI flags in addition to YAML.
- **Link modal streaming logs** — the web UI link modal now streams `lerd link` and `lerd env` output line-by-line instead of showing only a spinner.
- **Domain modal success feedback** — add/edit/remove domain operations in the web UI now show a flash message on success.
- **omarchy OS support** — systems with systemd-resolved but no NetworkManager can now install and run lerd. The installer accepts either resolver.
- **Reverb prerequisite check** — `lerd reverb:start` and `lerd reverb:stop` now check for `laravel/reverb` in composer.json before proceeding, with install instructions and a link to the Laravel Broadcasting docs.

### Changed

- **Worker state synced to `.lerd.yaml`** — all worker start/stop commands (`queue`, `schedule`, `reverb`, `horizon`, `stripe:listen`, `worker start/stop`) now persist the active workers list in `.lerd.yaml` when the file exists. Previously `worker start/stop` and `stripe:listen` did not update the file.
- **`lerd start` restores site infrastructure** — after an uninstall/reinstall cycle, `lerd start` reads `.lerd.yaml` from each active site and recreates missing FPM quadlets, service quadlets, and worker units automatically.
- **`lerd install` restores FPM quadlets** — reinstalling now restores PHP-FPM quadlets for all PHP versions used by registered sites, not just the default version.
- **Improved `lerd uninstall`** — stops all `lerd-*` systemd units (workers, stripe listeners, etc.) instead of only the hardcoded watcher and UI services. DNS teardown and the data-removal prompt now run before the step runner to avoid stdin conflicts.

### Fixed

- **DNS teardown leaves stale DNS on virtual interfaces** — `lerd uninstall` now reverts all network interfaces that have lerd DNS configured (e.g. `virbr0`, `vnet*`), not just the default interface.
- **Internet DNS broken after uninstall** — after reverting interfaces and restarting NetworkManager, lerd now explicitly pushes the DHCP-assigned upstream DNS servers so name resolution works immediately.
- **Domain modal stale state** — the web UI domain modal now properly updates the domain list after add/edit/remove operations. The site list merge was matching by domain (which changes) instead of name (stable).
- **`lerd env` runs automatically in setup** — `lerd env` now runs at the start of `lerd setup` instead of being a selectable step, ensuring `.env` is configured before `composer install` triggers post-install scripts.
- **Definition conflict resolution** — when `.lerd.yaml` and the local framework/service definition differ, lerd now offers a three-way choice: use .lerd.yaml version, use local definition, or skip. Both sync directions persist immediately.
- **Improved horizon/reverb error messages** — error messages now include install commands and docs links instead of generic text.
- **Dynamic DNS resolver hints** — `lerd doctor` and `lerd status` now show the correct restart command based on the active resolver instead of always suggesting "restart NetworkManager".

### Docs

- Added contributing section to nav bar, stripe page to usage sidebar, troubleshooting to reference sidebar
- Fixed `{{site}}` placeholders being swallowed by VitePress (Vue template interpolation)
- Replaced non-rendering mermaid chart with ASCII diagram on architecture page
- Added reverb prerequisite note to commands reference
- Updated requirements, architecture, and troubleshooting for systemd-resolved support

---

## [1.5.1] — 2026-04-04

### Fixed

- **Nginx fails to start when TLS certificates are missing** — `lerd start` now detects SSL vhosts referencing missing cert files before starting nginx, switches affected sites back to HTTP, and removes orphan SSL configs. Previously a single missing certificate would prevent all sites from loading.
- **Paused sites bypass landing page after update** — `lerd install` (called by `lerd update`) was regenerating vhosts for all sites, overwriting paused landing pages with the full site config. Paused and ignored sites are now skipped during vhost regeneration.
- **Paused landing page redesigned** — the paused page now matches the branded "Site Not Found" page with the Lerd logo, red accent, and Resume + Dashboard buttons. Uses a single shared HTML file instead of generating one per site.

---

## [1.5.0] — 2026-04-04

### Added

- **Multi-domain support** — sites can now respond to multiple `.test` domains. Use `lerd domain add`, `lerd domain remove`, and `lerd domain list` to manage them. Domains are stored in `.lerd.yaml` and the certificate is reissued automatically when a domain is added to a secured site.
- **`lerd env:check` command** — compare all `.env` files against `.env.example` and flag missing or extra keys. Exits non-zero when required keys are missing.
- **`lerd check` command** — validate `.lerd.yaml` syntax, PHP version, Node version, services, frameworks, and workers before running setup. Reports OK/WARN/FAIL per field.
- **`lerd which` command** — show the resolved PHP version, Node version, document root, and nginx config paths for the current site.
- **Port conflict detection** — `lerd start` checks for port conflicts before starting containers and warns if another process is already using a required port.
- **`lerd update --beta`** — update to the latest pre-release build from GitHub.
- **`lerd update --rollback`** — revert to the previously installed version using the automatic backup.
- **Automatic PHP/Node version switching** — the watcher monitors `.lerd.yaml`, `.php-version`, `.node-version`, and `.nvmrc` and automatically re-links the site when versions change.
- **Workers in `lerd init`** — the wizard includes a workers step that pre-selects workers based on the framework and installed packages. Horizon is auto-detected from `composer.json`.
- **Setup prompt on link** — when linking a site with workers configured in `.lerd.yaml`, lerd prompts to run `lerd setup` to install dependencies and start workers.
- **Branded error pages** — requests to unlinked `.test` domains show a styled "Site Not Found" page with links to the dashboard instead of a generic browser error.
- **Failing worker visibility** — `lerd status` shows failing and restarting workers across all sites. The web UI shows a pulsing red toggle and a "!" indicator on the log tab for failing workers.

### Fixed

- **Crash-looping workers left running after unlink** — `lerd unlink` now detects and stops crash-looping workers for the site.
- **Paused sites counted in status workers section** — paused sites are now excluded from the workers list in `lerd status`.
- **Paused sites counted in TLS check** — `lerd status` no longer flags TLS issues for paused or ignored sites.
- **Service container left behind on remove** — `lerd service remove` now properly cleans up the Podman container.

---

## [1.4.2] — 2026-04-03

### Fixed

- **Paused sites counted in service badges and auto-stop logic** — paused sites were included when counting how many sites use a service, so services stayed active and their site-count badges inflated even after all active sites were paused. Paused sites are now excluded from `CountSitesUsingService` and the badge tooltip list.

---

## [1.4.1] — 2026-04-03

### Fixed

- **3-pane dashboard layout missing from v1.4.0** — the new icon rail, list panel, and full-height detail panel were lost during a merge conflict resolution. The correct UI is now restored.

---

## [1.4.0] — 2026-04-03

### Added

- **3-pane dashboard layout** — the UI is redesigned around a persistent icon rail (Sites, Services, System), a scrollable list panel, and a full-height detail panel. Logs fill remaining height rather than being capped at a fixed box. Works at any scale from 1 to 50+ sites. Mobile gets a full-screen list/detail with a bottom tab bar and a back button.
- **PHP-FPM auto-lifecycle** — FPM containers for unused PHP versions are stopped automatically on `lerd unlink` and `lerd start`. Paused sites keep their FPM running. On `lerd start`, only versions referenced by at least one site are started. When a site is unpaused, its FPM container is guaranteed running before nginx is restored.
- **Manual FPM start/stop from the dashboard** — unused PHP versions (no active sites) show a Stop button in the dashboard when running. Stopped unused versions are shown with a neutral badge rather than an error.
- **`lerd start` parallel spinner UI** — start and stop operations now show a live per-unit progress display. All images required by units are checked and rebuilt or pulled before containers start.
- **Site pills on services** — core services (MySQL, Mailpit, etc.) and worker-type services (Queue, Horizon, Reverb, etc.) show clickable site pills. Clicking a pill navigates directly to that site's settings.
- **Clickable PHP-FPM site pills** — site pills on the PHP-FPM detail panel now navigate to the site's settings panel instead of opening the browser.
- **Instant system theme switching** — when the theme is set to Auto, the dashboard switches between light and dark immediately as the OS preference changes, without a page reload.

### Fixed

- **`lerd status` false errors for stopped unused PHP-FPM** — stopped FPM containers for versions not referenced by any site are now reported as warnings, not errors.
- **MinIO migration prompt shown after already migrating to RustFS** — the `lerd update` migration prompt now also checks whether the `lerd-minio` container is running, so users who have already migrated are not prompted again.
- **Pre-built PHP base images required ghcr.io login** — lerd now always pulls base images anonymously to avoid authentication errors from expired or unrelated ghcr.io credentials.

---

## [1.3.3] — 2026-04-02

### Fixed

- **Broadcasting jobs fail when `lerd env` was run on a Reverb site** — `REVERB_HOST` was set to the site domain (e.g. `my-app.test`), which resolves inside the PHP-FPM container to `host.containers.internal` (169.254.1.2). That address — the nginx proxy on the host — is not reachable from inside the container's network namespace, so every broadcast job failed with cURL error 7. `REVERB_HOST`, `REVERB_PORT`, and `REVERB_SCHEME` are now always written as `localhost`, `REVERB_SERVER_PORT`, and `http` so the queue worker connects to Reverb directly inside the same container. `VITE_REVERB_HOST/PORT/SCHEME` continue to use the site domain and external port for browser connections through nginx. Sites affected can be fixed by re-running `lerd env`.
- **Log lines repeating on SSE reconnect** — when the browser reconnected to a log stream (network blip, tab restore) the entire history was replayed from the start. For systemd units the stream now emits the journalctl cursor as the SSE event id and resumes with `--after-cursor` on reconnect; for Podman containers a monotonic line counter is used and `--tail 0` skips history on reconnect.

---

## [1.3.2] — 2026-04-01

### Fixed

- **Queue log streaming was a stale duplicate of the shared implementation** — the `/api/queue/<site>/logs` SSE handler had its own inline copy of the log streaming logic instead of calling the shared `streamUnitLogs` helper used by every other worker (horizon, schedule, reverb, stripe). The duplicate is removed.

---

## [1.3.1] — 2026-04-01

### Fixed

- **PHP FPM fails to start on fresh installs** — the shared hosts file (`~/.local/share/lerd/hosts`) is bind-mounted into every PHP-FPM container. If no site had ever been linked, the file did not exist and podman refused to start the container with `statfs: no such file or directory`. `WriteFPMQuadlet` now ensures the file is created before the container is started.

---

## [1.3.0] — 2026-04-01

### Added

- **Multiple Reverb sites without port collisions** — when `lerd env` detects `BROADCAST_CONNECTION=reverb`, it auto-assigns a unique `REVERB_SERVER_PORT` per site starting at 8080 and incrementing for each additional site. `reverb:start` (including the UI toggle) also assigns and persists the port on first start if still missing, so the fix applies even when `lerd env` has not been re-run. The nginx WebSocket proxy uses the per-site port instead of the old hardcoded 8080. Fixes [#47](https://github.com/geodro/lerd/issues/47).
- **New MCP tools: `db_import`, `db_create`, `php_list`, `php_ext`, `park`, `unpark`** — six new tools for AI agents covering database import from a SQL file, on-demand database creation, listing installed PHP versions, managing PHP extensions, and parking/unparking directories.
- **`lerd whatsnew`** — new command that prints the changelog for the currently installed version. The changelog excerpt has been removed from `lerd status` and `lerd doctor` output.
- **Portable `.lerd.yaml`** — `.lerd.yaml` can now describe a site's full local environment (PHP version, Node version, framework, services, custom workers). Running `lerd link` in a project that has a `.lerd.yaml` applies all settings automatically, so cloning a project and running `lerd link && lerd env` is enough to reproduce the full environment. Closes [#33](https://github.com/geodro/lerd/issues/33).
- **Pre-built PHP base images** — PHP images are now built on top of pre-built base images pulled from `ghcr.io` instead of compiling all extensions from source. First-install time drops from ~5 minutes to ~30 seconds. Closes [#43](https://github.com/geodro/lerd/issues/43).

---

## [1.2.4] — 2026-03-31

### Added

- **`lerd php:rebuild` accepts a version argument** — pass a version (e.g. `lerd php:rebuild 8.3`) to rebuild only that PHP image instead of all installed versions.

### Fixed

- **Inter-application `.test` domain resolution inside containers** — HTTP/HTTPS requests from one site to another (e.g. `booking.test` calling `staffing.test`) were failing because `.test` domains resolved to `127.0.0.1` inside containers, which points to the container itself rather than the host Nginx. A shared hosts file (`~/.local/share/lerd/hosts`) is now bind-mounted into every PHP-FPM container at `/etc/hosts` with a `169.254.1.2` entry per linked site. Since it is a bind mount, `lerd link` and `lerd unlink` update all running containers instantly without a restart. Fixes [#39](https://github.com/geodro/lerd/issues/39).
- **Reverb proxy returns 502 after container restart** — the Nginx `location /app` block used a bare hostname in `proxy_pass`, which Nginx resolves once at config load time. If the PHP-FPM container restarted and received a new IP, subsequent WebSocket and broadcast requests failed with 502. The proxy now uses a variable (`set $reverb`) to force per-request DNS resolution, matching how the FastCGI location already handles the FPM upstream.

---

## [1.2.3] — 2026-03-31

### Added

- **Horizon appears in the Services panel** — when Laravel Horizon is running for a site it now shows up as its own entry in the Services panel (grouped under "Horizon"), with a stop button, live log stream, and a subtitle showing the site domain. Previously Horizon was only visible in the site detail view.
- **Starting Horizon stops the queue worker** — `horizon:start` (CLI, UI, MCP) now automatically stops any running queue worker for the same site before starting Horizon, since the two must not run simultaneously.
- **`lerd unlink` stops all workers for the site** — queue workers, Horizon, schedule workers, Reverb, Stripe listeners, and custom framework workers are all stopped before the site is unlinked.

### Fixed

- **Tray no longer shows per-site workers** — Reverb, Horizon, queue workers, schedule workers, Stripe listeners, and custom framework workers are filtered out of the tray menu. Only real infrastructure services (MySQL, Redis, Mailpit, etc.) are listed there.
- **`lerd php` can now run scripts outside `$HOME`** — IDEs like PhpStorm write their validation scripts to `/tmp` and call `php -d... /tmp/ide-phpinfo.php`. The container only mounts `$HOME`, so those scripts were unreachable and produced an empty output ("Failed to parse validation script output"). `runPhp` now detects any argument that is an absolute path to a host file outside `$HOME`, reads it, and streams it to the container via `stdin` / `/dev/stdin`.
- **Horizon logs in the Services panel now stream the correct site** — the logs URL for a Horizon service entry now routes to `/api/horizon/{site}/logs` (systemd journal) instead of the generic `/api/logs/lerd-horizon-{site}` endpoint that tried to use `podman logs` on a non-existent container.
- **Horizon log tab on the Sites panel no longer shows stale logs from a previous site** — switching sites now properly closes and clears the Horizon log stream; clicking the Horizon tab reconnects to the correct site's stream.

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
