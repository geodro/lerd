## Lerd — Laravel Local Dev Environment

This project runs on **lerd**, a Podman-based Laravel development environment. The `lerd` MCP server is available — use it to manage the environment without leaving the chat.

The MCP surface is **eleven grouped tools**, each driven by an `action` argument: `site`, `service`, `db`, `env`, `runtime`, `worker`, `exec`, `framework`, `diag`, `logs`, `worktree`. Always pass `action`. Most actions also accept an optional `path` that defaults to the directory the assistant was opened in (then `LERD_SITE_PATH` if set), so you can usually omit it. Start by calling `site` with `action: "list"` to discover sites.

### Architecture

- PHP runs in Podman containers named `lerd-php<version>-fpm` (e.g. `lerd-php84-fpm`); each container includes composer and node/npm; the PHP version is resolved from `.lerd.yaml` → `.php-version` → `composer.json` `require.php` constraint (matched against installed versions) → global default
- Nginx routes `*.test` domains to the correct PHP-FPM container
- Services (MySQL, Redis, PostgreSQL, etc.) and custom services run as Podman containers via systemd quadlets
- Node.js versions are managed by fnm; per-project version is set via a `.node-version` file
- Framework workers (queue, schedule, reverb, horizon, messenger, vite, etc.) run as systemd user services named `lerd-<worker>-<sitename>`; commands are defined per-framework in YAML; Laravel Horizon is auto-detected from `composer.json` and replaces the queue toggle when installed; Laravel ships with a `vite` host worker that runs `npm run dev` on the host via fnm for HMR (it runs `bun run dev` instead when the project uses bun or Node is unmanaged and bun is installed); workers and setup commands support optional `check` (`file` or `composer`) for conditional visibility; workers with `conflicts_with` auto-stop conflicting workers on start. Per-worker flags: `host: true` (run on host via fnm instead of in FPM container — HMR-sensitive Node tools), `per_worktree: true` (worker runs independently per worktree under `lerd-<worker>-<site>-<branch>`), `replaces_build: true` (worker provides asset manifest while running, so a worktree add skips the static `npm run build` step when this worker is opted in)
- Custom workers can be added per-project (`.lerd.yaml` `custom_workers`) or globally (`~/.config/lerd/frameworks/<name>.yaml`); use the `worker` tool's `add`/`remove` actions — both survive framework store updates
- Framework setup commands (one-off bootstrap steps like migrations, storage links) are defined in the framework YAML and shown by `framework` `action: "setup"`; Laravel has built-in storage:link/migrate/db:seed; custom frameworks can define their own
- Service version placeholders (`{{mysql_version}}`, `{{postgres_version}}`, `{{redis_version}}`, `{{meilisearch_version}}`) are available in framework env vars and resolved from the service image tag at env-setup time
- **Custom containers**: non-PHP sites (Node.js, Python, Go, etc.) can define a `Containerfile.lerd` and a `container:` section in `.lerd.yaml` with a port; lerd builds a per-project image, runs it as `lerd-custom-<sitename>`, and nginx reverse-proxies to it; the project directory is volume-mounted at its host path with `--workdir` set automatically — do NOT add `WORKDIR` or `COPY` to the Containerfile; workers exec into the custom container; services are accessible by name on the shared `lerd` Podman network; **hot-reload file watchers must use polling on macOS** (inotify does not fire across Podman Machine's virtiofs mount) — nodemon: `--legacy-watch`, Vite: `server.watch.usePolling: true`, webpack: `watchOptions: { poll: 1000 }`
- **Custom-image PHP sites (custom-FPM)**: a PHP project can define a `Containerfile.lerd` (must build `FROM lerd-php<ver>-fpm:local`) plus a `container:` section with **no port**; lerd builds a per-site image (`lerd-custom-<site>:local`), runs a dedicated FPM container `lerd-cfpm-<site>`, and serves it by fastcgi instead of the shared `lerd-php<ver>-fpm`. It is a normal PHP site otherwise (xdebug, dumps, profiler, `lerd shell`, `php`/`artisan`/`composer`/`tinker`, queue/horizon all run in the per-site container). The PHP version is fixed by the `FROM` line (the UI version selector is read-only); `lerd rebuild` rebuilds the image. Same key as custom containers, the port is the discriminator: with a port it is a reverse-proxied non-PHP app, without a port it is a fastcgi PHP image. `runtime` for these reports `fpm-custom`.
- Git worktrees automatically get a `<branch>.<site>.test` subdomain (deep `*.<branch>.<site>.test` wildcard cert + nginx `server_name` on secured sites); `vendor/`, `node_modules/`, `.env` are seeded from the main checkout. `.lerd.yaml` `env_overrides` declares templated env vars (`{{domain}}`, `{{scheme}}`, `{{site}}`) layered on the default `APP_URL` rewrite — for multi-tenant apps (per-branch cookies, signed-URL hosts, tenant routing)

### DNS modes

Lerd has two install-time DNS modes recorded in `~/.config/lerd/config.yaml`:
- **Managed (default)**: `dns.enabled: true`, `dns.tld: test`. Sites at `*.test` via lerd-dns + mkcert; `site` `tls_enable` works.
- **Disabled**: `dns.enabled: false`, `dns.tld: localhost`. Sites at `*.localhost` via RFC 6761; no mkcert CA, TLS toggling unavailable.

Read `diag` `action: "status"` for `dns.tld` and `dns.enabled` instead of assuming `.test`; do not propose `tls_enable` when `dns.enabled` is false.

### MCP tools

Eleven grouped tools, each selecting behaviour via `action`.

#### `site` — sites and their configuration
Actions: `list` (discover sites — CALL FIRST), `link`, `unlink`, `domain_add`, `domain_remove`, `group_assign`, `group_unassign`, `group_label`, `group_db`, `group_list`, `tls_enable`, `tls_disable`, `php`, `node`, `pause`, `unpause`, `restart`, `rebuild`, `runtime`, `nginx_read`, `nginx_write`, `nginx_reset`, `park`, `unpark`.
- `link` registers a directory; non-PHP sites need `.lerd.yaml` `container.port` + a Containerfile first, or they register as PHP (wrong)
- `domain_*` take a domain without the `.test` TLD; you can't remove the last domain
- `group_*` nest a secondary site under a main's subdomain (one level deep): they identify the secondary by `path` (defaults to cwd), not by `site`; `group_assign` with `main` + `label` (+ optional `share_db`), `group_db` = share|separate
- `php`/`node` take `version`; pass `branch` to pin the override on a worktree's checkout
- `runtime` switches `fpm` ↔ `frankenphp` (`worker: true` enables frankenphp worker mode)
- `nginx_write` saves a custom override (runs `nginx -t`, backs up, reloads); `branch` targets a worktree
- `park` registers a parent dir and auto-registers every PHP project under it; `unpark` reverses it (project files kept)

#### `service` — built-in & custom services
Actions: `start`, `stop`, `restart`, `pin`, `unpin`, `update`, `rollback`, `migrate`, `remove`, `reinstall`, `add`, `expose`, `env`, `config_read`, `config_write`, `config_restore`, `config_reset`, `config_list_backups`, `preset_list`, `preset_install`, `check_updates`.
- `update` pulls a newer image (safe, in-strategy); `migrate` dumps + restores across a cross-strategy upgrade; `reinstall` with `reset_data: true` wipes data and reprovisions; `remove` with `remove_data: true` renames the data dir aside
- `stop` marks the service paused — `lerd start` skips it until started again; `pin` keeps it always running
- `add` registers a custom OCI service (`depends_on` wires dependencies, `init: true` for mysql/mariadb); prefer `preset_install` for anything in `preset_list` (phpmyadmin, pgadmin, mongo, mongo-express, selenium, stripe-mock, mysql, mariadb…)
- `env` returns the recommended `.env` connection keys; `expose` publishes an extra port
- `config_*` read/write/restore/reset a service's runtime tuning override

#### `db` — databases
Actions: `set`, `move`, `create`, `export`, `import`, `snapshot`, `snapshots`, `restore`, `snapshot_delete`.
- `set` picks the project DB (`database`: sqlite, mysql, postgres, or a family alternate like mariadb / postgres-pgvector / mysql-5-7); persists to `.lerd.yaml`, rewrites `DB_` keys, starts the service, creates the DB + `_testing`
- `move` migrates sites between two installed same-family services (`from`/`to`, `sites: [...]` or `all: true`) and repoints each `.env`; source data is left intact
- `create`/`export`/`import` auto-detect service and database; pass `service` to override
- `snapshot`/`snapshots`/`restore`/`snapshot_delete` are named, restorable snapshots (MySQL/MariaDB/PostgreSQL); `restore` is destructive; `all_databases` covers the whole service

#### `env` — .env management
Actions: `setup`, `check`, `override`.
- `setup` configures services, DBs, APP_KEY and APP_URL; on a fresh Laravel clone call `db` `set` first to move off sqlite, then `env setup`, then ALWAYS `framework setup` or migrations never run
- `check` compares `.env` against `.env.example`
- `override` manages the personal, gitignored `.env.lerd_override` (its `set` KEY=VALUE win over lerd defaults; `LERD_EXTERNAL_SERVICES=<svc,svc>` marks vars lerd writes but won't start)

#### `runtime` — PHP/Node versions & extensions
Actions: `versions`, `node_install`, `node_uninstall`, `php_list`, `ext_list`, `ext_add`, `ext_remove`.
- `ext_add`/`ext_remove` rebuild the FPM image and restart the container (slow); `ext_add` accepts `apk_deps` for extra Alpine build packages
- **extra Alpine packages**: `lerd php:pkg add/remove/list <packages> [--php version]` (CLI) bakes runtime apk packages (CLI tools, libs) into the FPM image, saved in config under `php.packages` and re-applied on every rebuild, so they survive `php:rebuild` and base image updates. Layered onto the shared image, not the published base.
- **Pest browser testing**: `lerd pest:browser install [version]` (CLI) sets up `pestphp/pest-plugin-browser` to run inside the shared FPM container. Pest drives Playwright locally and its bundled Chromium is glibc (can't run on the container's musl), so lerd bakes Alpine's musl `chromium` into the FPM image (via the `php:pkg` package mechanism, saved under `php.packages`), persists the Playwright registry in a `/root/.cache/ms-playwright` volume, and shims Playwright's browser to exec the system chromium with `--no-sandbox`. Chromium only (no musl Firefox/WebKit); current PHP versions only, not the legacy 7.4/8.0 tier (rejected up front). `lerd pest:browser doctor [version]` checks the setup; `lerd pest:browser remove [version]` un-bakes chromium and rebuilds (the cache volume is left intact). install fails fast if the `playwright` npm package is missing, before any rebuild. Re-run install after bumping the project's Playwright version. Tests then run via the normal `lerd test`/`lerd pest`.
- **bun**: lerd never installs or version-manages bun (user installs it; `bun upgrade` self-updates). On the host, JS install/dev/build run through bun when the project is a bun project (`bun.lockb`/`bun.lock`/`bunfig.toml`/`packageManager: bun`) or when Node is unmanaged and no system Node exists but bun is present. CLI-only host toggles: `lerd node:manage` / `lerd node:unmanage` opt into/out of fnm-managed Node and regenerate host workers (bun ↔ fnm); `lerd js:runtime [bun|node|auto]` pins a single site's JS runtime in `.lerd.yaml` (the dashboard's bun/Node toggle). For an in-container bun (for `lerd shell`): `lerd php:bun install` drops a musl bun into a persistent `/root/.bun` volume that survives image rebuilds, `lerd php:bun update` upgrades it in place, `lerd php:bun version` reports it; auto-installed on `lerd link`/`setup` when host bun is present. These are CLI/host operations, not container exec actions.

#### `worker` — background workers
Actions: `list` (CALL FIRST), `start`, `stop`, `add`, `remove`, `health`, `heal`, `mode_get`, `mode_set`, and the framework workers `queue_start`, `queue_stop`, `horizon_start`, `horizon_stop`, `reverb_start`, `reverb_stop`, `schedule_start`, `schedule_stop`, `stripe_start`, `stripe_stop`, `stripe_config`.
- call `list` to discover a site's workers before `start`; pass `branch` to target a per-worktree unit
- use `horizon_*` instead of `queue_*` when laravel/horizon is installed (mutually exclusive); `queue_start` needs Redis running when `QUEUE_CONNECTION=redis`
- `add` saves a custom worker to `.lerd.yaml` (or the user overlay with `global: true`); does not auto-start
- `health` lists failed units (read-only); `heal` resets and restarts them (`unit` for one, omit for all); `mode_get` reports the macOS worker runtime, `mode_set` switches it (`mode`: exec|container)
- Stripe secret is read from `.env` (STRIPE_SECRET / STRIPE_SECRET_KEY / STRIPE_API_KEY); `stripe_config` sets webhook_path / secret_env_key in `.lerd.yaml`
- **Auto-reload (CLI-only)**: `lerd horizon:reload [on|off]` and `lerd octane:reload [on|off]` (FrankenPHP worker mode) restart workers on file changes; both need the project's `chokidar` npm package
- **Idle-suspend (CLI-only)**: `lerd idle on/off` toggles activity-driven suspension globally; suspended workers stop after the idle timeout (`lerd idle timeout <dur>`) and resume on the next request/CLI/MCP/file-save. `lerd idle pin/unpin <site>` exempts a site; `lerd idle status` reports policy and last-active. A worker shown as suspended is healthy, not failed, so do not `heal` it

#### `exec` — run tooling in the PHP-FPM container
Actions: `artisan` (Laravel), `console` (other frameworks), `composer`, `vendor_bins`, `vendor_run`, `commands_list`, `commands_run`, `command_add`, `command_remove`.
- `artisan`/`console`/`composer` take `args` (array); tinker must use `--execute=<code>` for non-interactive use
- `vendor_run` is the right way to run project tooling (pest, phpunit, pint, phpstan, rector) — call `vendor_bins` first to discover what's installed, then `vendor_run` with `bin` + `args`; prefer it over `composer exec`
- `commands_*`/`command_*` list, run, add and remove the on-demand commands in a site's `.lerd.yaml` `commands:` block; `commands_run` needs `force: true` for confirm-gated commands

#### `framework` — framework definitions & scaffolding
Actions: `list`, `add`, `remove`, `search`, `install`, `project_new`, `setup`.
- `add` with `name: "laravel"` merges custom workers/setup into the built-in framework
- `search`/`install` use the community store (install auto-detects version from `composer.lock`)
- `project_new` scaffolds a new project (requires absolute `path`, default framework laravel); follow with `site` `link` + `env` `setup`
- `setup` runs the framework's post-install steps (migrations, storage:link…) — MANDATORY after `env setup` on new/cloned projects; idempotent

#### `diag` — diagnostics & observability
Actions: `status`, `doctor`, `which`, `check`, `dns_diagnose`, `bug_report`, `analyze_queries`, `dumps_recent`, `dumps_status`, `dumps_clear`, `dumps_toggle`, `profiler_toggle`, `profiler_status`, `profiler_clear`, `xdebug_on`, `xdebug_off`, `xdebug_status`.
- `status` (DNS/nginx/FPM/watcher health) and `doctor` (full JSON diagnostic) are the first stops when something is broken; `dns_diagnose` walks the DNS chain
- reading logs lives in the `logs` tool (below), not here
- `which` shows resolved PHP/Node/docroot/nginx for a site; `check` validates `.lerd.yaml`
- debug bridge loop: `dumps_toggle` (enable) → `dumps_clear` → hit the page → `analyze_queries` (N+1 / slow-query report with file:line) or `dumps_recent` (filter by site/branch/ctx/kind/since/limit)
- `profiler_*` toggle the global SPX profiler and surface the flame-graph UI; `xdebug_*` control Xdebug on port 9003 (`mode` defaults to debug)
- `bug_report` writes an anonymised diagnostic report for a GitHub issue

#### `logs` — read logs from any source, filtered
Actions: `sources`, `fetch`. Debug without opening files by hand.
- `sources` lists every queryable source for the site plus shared infra: `app:<file>` (framework log files), `fpm`, `worker:<name>` (queue/horizon/schedule/custom), and the globals `nginx`, `dns`, `watcher`, `ui`, services, `php<ver>`. Call it first to learn the names
- `fetch source=<name>` reads one source. Filter with `grep` (regex, falls back to literal substring), `since`/`until` (relative like `15m`/`1h`/`2h30m`, or a timestamp), `level` (app logs only: error/warning/info/debug), and `lines` (default 50)
- streaming is polling: every `fetch` returns an opaque `cursor`; call again with `since=<cursor>` (or `cursor=<cursor>`) to get only the new lines. The cursor format differs per backend, so treat it as opaque and echo it back
- entries come back chronological (oldest first). Raw logs with no timestamps ignore `since`/`level` and just return the last N; a not-running container returns partial output, not an error

#### `worktree` — git worktrees
Actions: `list`, `add`, `remove`, `db_isolate`, `db_share`.
- `add` installs deps and offers an asset-worker / npm-build prompt; secured sites get `*.<branch>.<site>.test` wildcard cert SANs + nginx `server_name` automatically
- `db_isolate` gives a worktree its own database (seed via `source`: empty|main|<branch>); `db_share` points it back at the main; `remove` keeps an isolated DB unless `keep_db: false`

### Key conventions

- Pass `action` on every tool; `path` is optional on most and defaults to the directory the assistant was opened in
- Discover before acting: `site` `list` for sites, `worker` `list` for a site's workers, `service` `preset_list` before `preset_install`, `exec` `vendor_bins` before `vendor_run`
- On a fresh Laravel clone (DB_CONNECTION=sqlite), call `db` `set` before `env` `setup` to choose a database deliberately, then run `framework` `setup`
- **Domain conflicts on link**: the parked-directory watcher filters out a domain another site already owns and prints `[WARN] domain "X" already used by site "Y" — skipped`, registering the site with surviving domains (falling back to `<dirname>.<tld>`); `.lerd.yaml` is not modified. The `site` `link` and `site` `domain_add` actions instead hard-error on conflicts so you can react — read the error for the owning site name
- **Custom APP_URL**: `env` `setup` writes `<scheme>://<primary-domain>`; override via `app_url` in `.lerd.yaml` (committed) or the per-machine `sites.yaml` entry, then re-run `env setup`
- Built-in service hosts follow `lerd-<name>` (e.g. `lerd-mysql`, `lerd-redis`, `lerd-postgres`); default DB credentials are username `root`, password `lerd`
- **Custom container sites** (Node.js, Python, Go, …) — mandatory order: (1) write a Containerfile (default `Containerfile.lerd`); (2) write `.lerd.yaml` with `container: {port: <N>}` (plus optional `domains`, `services`, `secured`); (3) configure the project's `.env` with service hosts (`lerd-mysql`, etc.) and start needed services via `service` `start`; (4) call `site` `link`. Never link before steps 1–3 or the site registers as PHP-FPM; if that happens, `site` `unlink`, write the files, then link again
- Worker unit names follow `lerd-<worker>-<site>` (per-worktree: `lerd-<worker>-<site>-<branch>`)
