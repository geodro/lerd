# Command Reference

## Setup & lifecycle

| Command | Description |
|---|---|
| `lerd install` | One-time setup: directories, network, binaries, DNS, nginx, watcher |
| `lerd start` | Start DNS, nginx, PHP-FPM containers, and all installed services; warns about port conflicts and builds or pulls any missing images first |
| `lerd stop` | Stop nginx, PHP-FPM containers, and all running services; leaves the `lerd-dns` forwarder running as install-level plumbing so `.test` keeps resolving |
| `lerd quit` | Stop all Lerd processes and containers including the UI, watcher, tray, and the `lerd-dns` forwarder; on macOS also stops the Podman Machine VM |
| `lerd update` | Check for updates and update after confirmation |
| `lerd update --beta` | Update to the latest pre-release build |
| `lerd update --rollback` | Revert to the previously installed version |
| `lerd whatsnew` | Show what changed between the installed version and the latest release |
| `lerd uninstall` | Stop all containers and remove Lerd |
| `lerd uninstall --force` | Same, skipping all confirmation prompts |
| `lerd autostart enable` | Start Lerd automatically on every login |
| `lerd autostart disable` | Disable autostart on login |
| `lerd tray` | Launch the system tray applet (detaches from terminal) |
| `lerd dns:check` | Walk the DNS chain (container, dnsmasq config, port 5300, dig at 5300, resolver hookup, interface routing, system lookup) and print the layered status with a remediation hint per failure |
| `lerd status` | Health summary: DNS, nginx, PHP-FPM containers, watcher, services, cert expiry, LAN exposure and dashboard remote access; shows a notice if an update is available |
| `lerd which` | Show resolved PHP version, Node version, document root, and nginx config for the current site |
| `lerd about` | Show version, build info, and project URL |
| `lerd man [page]` | Browse the built-in documentation in the terminal; pass a page name to jump directly (e.g. `lerd man sites`) |
| `lerd tui` | Open a btop-style terminal dashboard with live site / service / worker status, per-site detail pane, inline domain and version editing, shell drop-in, log tailing, filter + sort, and global settings |
| `lerd check` | Validate `.lerd.yaml` syntax, services, and PHP version before setup |
| `lerd doctor` | Full environment diagnostic: podman, systemd, DNS, ports, PHP images, config validity; also reports how much podman disk is reclaimable |
| `lerd site:doctor [domain]` | App-level health checks for a single site (env file, env drift, application key, composer/node dependency install + lock, `composer audit`/`npm audit`, PHP version range, plus the framework's own checks). Defaults to the site in the current directory; pass a domain to target another. Add `--json` for machine-readable output |
| `lerd cleanup` | Reclaim podman disk from orphaned lerd images: old PHP build images and stale base images a rebuild left behind. Previews the list and confirms before removing. Only ever touches lerd's own images, never your databases, volumes, or other images |
| `lerd cleanup --dry-run` | Show what would be reclaimed and the approximate size, remove nothing |
| `lerd cleanup --deep` | Also remove unused service images no installed service references any more (e.g. an old `mysql:8.0` after upgrading), keeping each service's current image and its one-back rollback target |
| `lerd cleanup --yes` | Remove without the confirmation prompt |
| `lerd cleanup auto on` | Enable automatic cleanup (the default): the watcher's daily safe-tier sweep plus immediate reaping after a PHP rebuild or service update/remove |
| `lerd cleanup auto off` | Disable automatic cleanup; `lerd cleanup` still works on demand |
| `lerd cleanup auto status` | Show whether automatic cleanup is enabled |
| `lerd bug-report [-o file] [--log-lines n] [--show-real-names]` | Dump doctor output, config files, unit state, recent logs, network state and env vars to a plain-text file you can attach to a GitHub issue. Site names, domains, parked paths, home paths and the username are anonymized by default; `--show-real-names` keeps raw values |
| `lerd logs [-f] [target]` | Show logs for the current project's FPM container, `nginx`, a service name, or a PHP version |

## Project creation

| Command | Description |
|---|---|
| `lerd new <name-or-path>` | Scaffold a new PHP project using the framework's create command (default: Laravel) |
| `lerd new <name> --framework=<name>` | Scaffold using a specific framework |
| `lerd new <name> -- <extra args>` | Pass extra args to the scaffold command |

## Project setup

| Command | Description |
|---|---|
| `lerd init` | Wizard: choose PHP version, HTTPS, and services, save `.lerd.yaml`, apply |
| `lerd init --fresh` | Re-run the wizard with existing `.lerd.yaml` values as defaults |
| `lerd setup` | Bootstrap a project: runs the lerd init wizard first, then a checkbox list of steps |
| `lerd setup --all` | Run init (or apply saved `.lerd.yaml`) and all steps without prompting (useful in CI) |
| `lerd setup --skip-open` | Same as above but don't open the browser at the end |

Setup steps include common tasks (composer install, npm install, lerd env) plus framework-specific commands defined in the framework's `setup` field (e.g. migrations, storage links). See [Framework definitions](/usage/framework-definitions) for how to define custom setup commands.

## Site management

| Command | Description |
|---|---|
| `lerd park [dir]` | Register all Laravel projects inside `dir` (defaults to cwd) |
| `lerd unpark [dir]` | Remove a parked directory and unlink all its sites |
| `lerd link [name]` | Register the current directory as a site. On a fresh project with no `.lerd.yaml`, an interactive terminal routes through the `lerd init` wizard first (PHP version, HTTPS, services) before linking; prompts to import data when `laravel/sail` is detected in `composer.json`. **Non-PHP projects** (Node.js, Python, Go, etc.) must have `Containerfile.lerd` and `.lerd.yaml` with `container: {port: N}` already written before calling this, see [Custom Containers](../usage/custom-containers.md) |
| `lerd link [name] --domain foo.test` | Register with a custom domain |
| `lerd unlink [name]` | Stop serving the site |
| `lerd sites` | Table view of all registered sites |
| `lerd open [name]` | Open the site in the default browser |
| `lerd share [name]` | Expose the site publicly via ngrok, cloudflared, or Expose (auto-detected) |
| `lerd secure [name]` | Issue a mkcert TLS cert and enable HTTPS, updates `APP_URL` in `.env` |
| `lerd unsecure [name]` | Remove TLS and switch back to HTTP, updates `APP_URL` in `.env` |
| `lerd pause [name]` | Pause a site: stop workers (and custom container if applicable), replace vhost with landing page |
| `lerd unpause [name]` | Resume a paused site: start container, restore vhost, restart workers |
| `lerd restart [name]` | Restart the container for the current or named site (custom container or PHP-FPM) |
| `lerd rebuild [name]` | Rebuild the custom container image from Containerfile and restart |
| `lerd group add <main> <label>` | Group the current site under `<main>` (name or domain) at `<label>.<main-domain>`; add `--share-db` to share the main's database. See [Site Groups](../usage/site-groups.md) |
| `lerd group label <label>` | Change the current secondary's subdomain label |
| `lerd group db <share\|separate>` | Switch the current secondary between sharing the main's database and keeping its own |
| `lerd group remove` | Ungroup the current secondary, restoring a standalone domain |
| `lerd group list` | List all site groups and their members |
| `lerd env` | Configure `.env` for the current project with lerd service connection settings; backs up the original as `.env.before_lerd` on first run (skipped if lerd has already written to the file) |
| `lerd env:restore` | Restore `.env` from the pre-lerd backup (`.env.before_lerd`) |
| `lerd env:override [KEY=VALUE ...]` | Create/seed a personal, gitignored `.env.lerd_override` whose values win over lerd's defaults on `lerd env`; `LERD_EXTERNAL_SERVICES=` marks services lerd should not start or provision |
| `lerd env:check` | Compare all `.env` files against `.env.example` and flag missing or extra keys |

## LAN

### LAN sharing (per-site, no DNS setup required on clients)

| Command | Description |
|---|---|
| `lerd lan:share` | Start a LAN reverse proxy for the current site on a stable port; prints the URL and a QR code |
| `lerd lan:unshare` | Stop LAN sharing for the current site and release its port |

The proxy runs inside the lerd daemon (`lerd-ui`), no external tool needed and no internet access required. Any device on the same network can reach the site at `http://<your-LAN-IP>:<port>` without configuring DNS. The assigned port is stored in `sites.yaml` and reused across restarts. The proxy rewrites the Host header so nginx routes correctly, and rewrites absolute URLs in HTML/CSS/JS responses so asset and redirect URLs point to the LAN address instead of the `.test` domain. See [LAN sharing](/usage/lan-sharing) for details.

`lerd share` (without `lan:`) is different: it wraps an external tunnel tool (ngrok/cloudflared/Expose/SSH) to expose the site to the **public internet**.

### Full LAN exposure (all sites, DNS-based)

| Command | Description |
|---|---|
| `lerd lan:expose` | Expose all lerd services to the LAN: binds nginx to `0.0.0.0`, starts the DNS forwarder |
| `lerd lan:unexpose` | Restrict everything back to `127.0.0.1` |
| `lerd lan:status` | Show whether lerd is currently exposed to the local network |

See [Remote / LAN Development](/usage/remote-development) for the full walkthrough.

## PHP

Supported PHP versions: **8.5**, **8.4**, **8.3**, **8.2**, **8.1**, and the frozen legacy tier **8.0** and **7.4**. The legacy tier is opt-in only (you have to `lerd use 7.4` or `lerd isolate 7.4` explicitly), pulls from `php:7.4-fpm-alpine` / `php:8.0-fpm-alpine` upstream tags, and intentionally skips ext-mongodb (unavailable on those PHP versions). Use the legacy tier for hosted legacy apps; default new projects to 8.4 LTS or 8.5.

| Command | Description |
|---|---|
| `lerd use <version>` | Set the global PHP version and build the FPM image if needed |
| `lerd isolate <version>` | Pin PHP version for cwd: writes `.php-version` and updates `.lerd.yaml` if present, then re-links |
| `lerd php:list` | List all installed PHP-FPM versions |
| `lerd php:rebuild [--local]` | Force-rebuild all installed PHP-FPM images (pulls pre-built base by default; `--local` builds from source) |
| `lerd fetch [version...] [--local]` | Pull pre-built PHP FPM base images from ghcr.io for the given (or all supported) versions; `--local` builds from source instead |
| `lerd xdebug on [version] [--mode MODE] [--on-demand]` | Enable Xdebug for a PHP version. `--mode` defaults to `debug`; accepts `coverage`, `develop`, `profile`, `trace`, `gcstats`, or comma combos like `debug,coverage`. `--on-demand` sets `start_with_request=trigger` so nothing auto-connects |
| `lerd xdebug off [version]` | Disable Xdebug |
| `lerd xdebug status` | Show Xdebug enabled/disabled state and active mode for all installed PHP versions |
| `lerd xdebug pause [site] [--list] [--pid PID]` | (experimental, PHP-FPM sites only) Break the IDE debugger into a running worker/CLI process via Xdebug's control socket (`xdebugctl`). `--list` shows candidate processes, `--pid` targets one |
| `lerd php:ext add <ext> [version] [--apk-deps PKG[,PKG]]` | Add a custom PHP extension and rebuild the FPM image. `--apk-deps` accepts additional Alpine packages that the extension needs at build time (e.g. `--apk-deps libwebp-dev,libpng-dev` for `gd` with WebP support); the package list is persisted in `~/.config/lerd/config.yaml` so future rebuilds reapply it |
| `lerd php:ext remove <ext> [version]` | Remove a custom PHP extension and rebuild |
| `lerd php:ext list [version]` | List custom extensions for a PHP version |
| `lerd php:pkg add <package...> [--php VERSION]` | Add extra Alpine packages to the FPM image and rebuild; the list is persisted so future rebuilds reapply it |
| `lerd php:pkg remove <package...> [--php VERSION]` | Remove extra Alpine packages and rebuild |
| `lerd php:pkg list [--php VERSION]` | List the extra Alpine packages configured for a PHP version |
| `lerd php:ini [version]` | Open the user php.ini for a PHP version in `$EDITOR` |
| `lerd pest:browser install [version]` | Set up in-container Pest browser testing: bake musl chromium into the FPM image, download the Playwright registry into a persistent volume, and shim Playwright's glibc browser to it |
| `lerd pest:browser remove [version]` | Remove chromium from the FPM image and disable Pest browser testing (the Playwright cache volume is left intact) |
| `lerd pest:browser doctor [version]` | Diagnose the Pest browser testing setup (plugin, chromium, playwright, shim) for a PHP version |
| `lerd php:bun install [version] [--pin VERSION]` | Install (or update) a musl bun into the container's persistent `/root/.bun` volume, shared across every PHP version; `--pin` fixes a specific bun version instead of latest |
| `lerd php:bun update [version]` | Update the container's bun in place (`bun upgrade`) |
| `lerd php:bun version [version]` | Show the bun version installed in the PHP-FPM container |
| `lerd php:bun remove` | Remove the in-container bun and clear its persistent volume |
| `lerd dump on` | Enable the debug bridge so `dump()` / `dd()` calls ship to the lerd dashboard, TUI, and MCP tools |
| `lerd dump off` | Disable the debug bridge and restore FPM containers to their default state |
| `lerd dump status` | Show whether the bridge is enabled and how many events are buffered |
| `lerd dump tail [--site X] [--branch Y] [--ctx fpm\|cli]` | Stream captured dumps to the terminal until Ctrl-C |
| `lerd dump clear` | Clear the in-memory dump ring without disabling the bridge |
| `lerd profile on` | Turn the SPX profiler on so every PHP-FPM site's requests are profiled into flame graphs |
| `lerd profile off` | Turn the SPX profiler off |
| `lerd profile status` | Show whether the profiler is on and the SPX web UI URL |
| `lerd profile open` | Open the SPX profiler web UI in the browser |
| `lerd profile run <command> [args...]` | Profile a one-off CLI command (e.g. `lerd profile run artisan queue:work`) |
| `lerd profile clear` | Delete all captured SPX profile reports |
| `lerd notify on` | Enable lerd notifications globally (dashboard banners + Web Push fanout) |
| `lerd notify off` | Globally mute lerd notifications; bypasses per-device prefs |
| `lerd notify status` | Show whether notifications are globally enabled |

## Runtime

Switch the PHP runtime for the current site between shared PHP-FPM and per-site FrankenPHP. See the [FrankenPHP runtime](../features/frankenphp.md) page for adapters, worker mode, and limitations.

| Command | Description |
|---|---|
| `lerd runtime` | Print the current runtime for the site in cwd |
| `lerd runtime frankenphp` | Switch to per-site FrankenPHP (non-worker); writes `runtime: frankenphp` to `.lerd.yaml` |
| `lerd runtime frankenphp --worker` | Enable FrankenPHP with worker mode (Laravel Octane or Symfony's FrankenPHP adapter with `--watch`) |
| `lerd runtime frankenphp --no-worker` | Switch to FrankenPHP and explicitly disable worker mode |
| `lerd runtime fpm` | Back to shared PHP-FPM; clears the runtime field from `.lerd.yaml` |
| `lerd octane:reload [on\|off]` | Toggle Octane auto-reload on file changes (`octane:start --watch`) for the current FrankenPHP worker-mode site; with no argument prints the current state. Needs the `chokidar` npm package |

## Node

| Command | Description |
|---|---|
| `lerd node:install <version>` | Install a Node.js version globally via fnm |
| `lerd node:uninstall <version>` | Uninstall a Node.js version via fnm |
| `lerd node:use <version>` | Set the default Node.js version |
| `lerd isolate:node <version>` | Pin Node version for cwd: writes `.node-version`, runs `fnm install` |
| `lerd node [args...]` | Run `node` using the project's pinned version via fnm |
| `lerd npm [args...]` | Run `npm` using the project's pinned Node version via fnm |
| `lerd npx [args...]` | Run `npx` using the project's pinned Node version via fnm |
| `lerd js:runtime [bun\|node\|auto]` | Pin the current site's JS runtime in `.lerd.yaml` (the CLI equivalent of the dashboard's bun/Node toggle); with no argument prints the current runtime |

## Services

| Command | Description |
|---|---|
| `lerd service start <name>` | Start a service (auto-installs on first use) |
| `lerd service stop <name>` | Stop a service container |
| `lerd service restart <name>` | Restart a service container; refreshes the quadlet first so config edits take effect |
| `lerd service status <name>` | Show systemd unit status |
| `lerd service list` | All services with status, version, and an Update column showing pending updates |
| `lerd service update <name> [tag]` | Pull a newer image and restart; with no tag applies the safe in-strategy update, with a tag targets an explicit upgrade |
| `lerd service migrate <name> <target-tag>` | SQL dump + restore for cross-version mysql / postgres moves; old data dir and dump preserved under `~/.local/share/lerd/backups` |
| `lerd service rollback <name>` | Swap back to the previously-running image; toggles, so a second rollback redoes the update |
| `lerd service expose <name> <host:container>` | Publish an extra port on a built-in service (persisted, auto-restarts if running) |
| `lerd service expose <name> <host:container> --remove` | Remove a previously exposed port |
| `lerd service port <name> <port>` | Move a service's published host port without touching its container-internal port; persisted and auto-restarts if running |
| `lerd service port <name> --reset` | Reset a service to its preset default published port (same as `port <name> 0`) |
| `lerd service pin <name>` | Pin a service so it is never auto-stopped when no sites use it |
| `lerd service unpin <name>` | Unpin a service so it can be auto-stopped when unused |
| `lerd service add [file.yaml]` | Register a new custom service (from a YAML file or flags) |
| `lerd service preset [name]` | List bundled presets, or install one (use `--version` for multi-version presets) |
| `lerd service remove <name> [--purge]` | Stop and remove a service (custom or default). With `--purge`, also rename the data dir aside (recoverable as `<name>.pre-remove-<ts>`) |
| `lerd service reinstall <name> [--reset-data]` | Stop, remove, and reinstall at the current version. With `--reset-data`, rename the data dir aside and recreate linked sites' databases or buckets on the fresh service |
| `lerd minio:migrate` | Migrate existing MinIO data to RustFS |

## Database

| Command | Description |
|---|---|
| `lerd db:create [name]` | Create a database and a `<name>_testing` database |
| `lerd db:import [-d name] <file.sql>` | Import a SQL dump (defaults to site DB from `.env`) |
| `lerd db:export [-d name] [-o file.sql]` | Export a database to a SQL dump (defaults to site DB from `.env`) |
| `lerd db:shell` | Open an interactive MySQL or PostgreSQL shell |
| `lerd db:snapshot [name] [-A]` | Create a named, restorable snapshot of a database |
| `lerd db:snapshots [--all]` | List stored database snapshots |
| `lerd db:restore <name> [-A] [-f]` | Restore a database from a stored snapshot |
| `lerd db:snapshot:rm <name> [-A]` | Delete a stored database snapshot |
| `lerd db:move [--from svc] [--to svc] [--all\|--site name]` | Move sites' databases between two installed services in the same family and repoint their `.env`; wizard when run without flags |

## Import

| Command | Description |
|---|---|
| `lerd import sail` | Import database and S3/MinIO files from a Laravel Sail project into lerd |
| `lerd sail import` | Alias, natural order when already in a Sail project (`lerd sail <anything-else>` proxies to `vendor/bin/sail`) |
| `lerd import sail --skip-s3` | Import database only, skip S3/MinIO file mirroring |
| `lerd import sail --no-stop` | Leave Sail running after import completes |
| `lerd import sail --sail-db-name <name>` | Override the Sail-side database name (auto-detected by default) |

See [Importing from Laravel Sail](/usage/import-sail) for full documentation.

## Queue workers

| Command | Description |
|---|---|
| `lerd queue:start` | Start a queue worker for the current project |
| `lerd queue:stop` | Stop the queue worker for the current project |

## Horizon

For projects that use `laravel/horizon`, lerd detects it automatically from `composer.json`.

| Command | Description |
|---|---|
| `lerd horizon:start` | Start Laravel Horizon for the current project as a persistent background service |
| `lerd horizon:stop` | Stop Horizon |
| `lerd horizon:reload [on\|off]` | Toggle Horizon auto-reload on file changes for the current site; with no argument prints the current state. Needs the `chokidar` npm package |

## Reverb

Requires [Laravel Broadcasting](https://laravel.com/docs/13.x/broadcasting) with the `laravel/reverb` package, lerd detects it automatically from `composer.json`.

| Command | Description |
|---|---|
| `lerd reverb:start` | Start the Reverb WebSocket server for the current project as a persistent background service |
| `lerd reverb:stop` | Stop the Reverb server |

## Schedule

| Command | Description |
|---|---|
| `lerd schedule:start` | Start the task scheduler (`schedule:work`) for the current project as a persistent background service |
| `lerd schedule:stop` | Stop the task scheduler |

## Framework workers

| Command | Description |
|---|---|
| `lerd worker start <name>` | Start any named framework worker for the current project |
| `lerd worker stop <name>` | Stop a named framework worker |
| `lerd worker list` | List all workers defined for the current project's framework |

## Idle-suspend

Activity-driven worker suspension: lerd gracefully stops each site's suspendable workers (queue, scheduler, Horizon, Reverb, Stripe listener, Vite) after a period of no activity and resumes them on the next request, CLI command, MCP call, or source-file save. See the [idle-suspend](../usage/idle-suspend.md) page for the full behaviour.

| Command | Description |
|---|---|
| `lerd idle on` | Enable idle-suspend globally |
| `lerd idle off` | Disable idle-suspend and resume every suspended worker |
| `lerd idle status` | Show each site's idle-suspend policy and last-active time |
| `lerd idle timeout <duration>` | Set the idle timeout (e.g. `30m`, `2h`) |
| `lerd idle pin <site>` | Pin a site so idle-suspend never sleeps it |
| `lerd idle unpin <site>` | Unpin a site so idle-suspend can sleep it again |

## Framework definitions

| Command | Description |
|---|---|
| `lerd framework list` | List all available framework definitions and their workers |
| `lerd framework add <name>` | Add or update a framework definition (flags or `--from-file`) |
| `lerd framework remove <name>` | Remove a framework definition (confirms if a site still uses it) |
| `lerd framework prune` | Remove installed definitions no site uses |

## Stripe

| Command | Description |
|---|---|
| `lerd stripe:listen` | Start a Stripe webhook listener for the current project as a background service |
| `lerd stripe:listen stop` | Stop the Stripe webhook listener |
| `lerd stripe:config` | Show or set the webhook path and secret env key in `.lerd.yaml` without starting the listener |

## Console & runtime passthrough

| Command | Description |
|---|---|
| `lerd console [args...]` | Run the framework's console command (e.g., `php artisan` for Laravel, `php bin/console` for Symfony) inside the project's PHP-FPM container |
| `lerd artisan [args...]` | Alias for `lerd console`, equivalent to `php artisan` since the `php` shim also runs inside the FPM container |
| `lerd a [args...]` | Short alias for `lerd console` / `lerd artisan` |
| `lerd test [args...]` | Shortcut for `lerd artisan test` |
| `lerd <vendor-bin> [args...]` | Run any composer-installed binary from the project's `vendor/bin` directory (e.g. `lerd pest`, `lerd pint`, `lerd phpstan`). Real lerd commands always win over vendor binaries with the same name. |
| `lerd shell` | Open an interactive shell inside the project's PHP-FPM container |

## AI integration

| Command | Description |
|---|---|
| `lerd mcp:enable-global` | Register lerd MCP at user scope across every supported assistant (Claude Code, Cursor, Junie, Codex, Gemini, Copilot, Antigravity, Windsurf), available in every session regardless of directory |
| `lerd mcp:inject` | Inject the lerd MCP config and AI skill files into the current project |
| `lerd mcp:inject --path <dir>` | Inject into a specific project directory |

## Dashboard

| Command | Description |
|---|---|
| `lerd dashboard` | Open the Lerd dashboard (`http://127.0.0.1:7073`) in the default browser |

## Shell completion

```bash
lerd completion bash   # add to ~/.bashrc
lerd completion zsh    # add to ~/.zshrc
lerd completion fish   # add to ~/.config/fish/completions/lerd.fish
```
