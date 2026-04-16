# Command Reference

## Setup & lifecycle

| Command | Description |
|---|---|
| `lerd install` | One-time setup: directories, network, binaries, DNS, nginx, watcher |
| `lerd start` | Start DNS, nginx, PHP-FPM containers, and all installed services; warns about port conflicts and builds or pulls any missing images first |
| `lerd stop` | Stop DNS, nginx, PHP-FPM containers, and all running services |
| `lerd quit` | Stop all Lerd processes and containers including the UI, watcher, and tray |
| `lerd update` | Check for updates and update after confirmation |
| `lerd update --beta` | Update to the latest pre-release build |
| `lerd update --rollback` | Revert to the previously installed version |
| `lerd whatsnew` | Show what changed between the installed version and the latest release |
| `lerd uninstall` | Stop all containers and remove Lerd |
| `lerd uninstall --force` | Same, skipping all confirmation prompts |
| `lerd autostart enable` | Start Lerd automatically on every login |
| `lerd autostart disable` | Disable autostart on login |
| `lerd tray` | Launch the system tray applet (detaches from terminal) |
| `lerd dns:check` | Verify that `*.test` resolves to `127.0.0.1` |
| `lerd status` | Health summary: DNS, nginx, PHP-FPM containers, watcher, services, cert expiry, LAN exposure and dashboard remote access; shows a notice if an update is available |
| `lerd which` | Show resolved PHP version, Node version, document root, and nginx config for the current site |
| `lerd about` | Show version, build info, and project URL |
| `lerd man [page]` | Browse the built-in documentation in the terminal; pass a page name to jump directly (e.g. `lerd man sites`) |
| `lerd check` | Validate `.lerd.yaml` syntax, services, and PHP version before setup |
| `lerd doctor` | Full environment diagnostic — podman, systemd, DNS, ports, PHP images, config validity |
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
| `lerd init` | Wizard: choose PHP version, HTTPS, and services → save `.lerd.yaml` → apply |
| `lerd init --fresh` | Re-run the wizard with existing `.lerd.yaml` values as defaults |
| `lerd setup` | Bootstrap a project — runs the lerd init wizard first, then a checkbox list of steps |
| `lerd setup --all` | Run init (or apply saved `.lerd.yaml`) and all steps without prompting (useful in CI) |
| `lerd setup --skip-open` | Same as above but don't open the browser at the end |

Setup steps include common tasks (composer install, npm install, lerd env) plus framework-specific commands defined in the framework's `setup` field (e.g. migrations, storage links). See [Frameworks & Workers](/usage/frameworks) for how to define custom setup commands.

## Site management

| Command | Description |
|---|---|
| `lerd park [dir]` | Register all Laravel projects inside `dir` (defaults to cwd) |
| `lerd unpark [dir]` | Remove a parked directory and unlink all its sites |
| `lerd link [name]` | Register the current directory as a site; prompts to import data when `laravel/sail` is detected in `composer.json` |
| `lerd link [name] --domain foo.test` | Register with a custom domain |
| `lerd unlink [name]` | Stop serving the site |
| `lerd sites` | Table view of all registered sites |
| `lerd open [name]` | Open the site in the default browser |
| `lerd share [name]` | Expose the site publicly via ngrok, cloudflared, or Expose (auto-detected) |
| `lerd secure [name]` | Issue a mkcert TLS cert and enable HTTPS — updates `APP_URL` in `.env` |
| `lerd unsecure [name]` | Remove TLS and switch back to HTTP — updates `APP_URL` in `.env` |
| `lerd pause [name]` | Pause a site: stop workers (and custom container if applicable), replace vhost with landing page |
| `lerd unpause [name]` | Resume a paused site: start container, restore vhost, restart workers |
| `lerd restart [name]` | Restart the container for the current or named site (custom container or PHP-FPM) |
| `lerd env` | Configure `.env` for the current project with lerd service connection settings; backs up the original as `.env.before_lerd` on first run (skipped if lerd has already written to the file) |
| `lerd env:restore` | Restore `.env` from the pre-lerd backup (`.env.before_lerd`) |
| `lerd env:check` | Compare all `.env` files against `.env.example` and flag missing or extra keys |

## LAN

### LAN sharing (per-site, no DNS setup required on clients)

| Command | Description |
|---|---|
| `lerd lan:share` | Start a LAN reverse proxy for the current site on a stable port; prints the URL and a QR code |
| `lerd lan:unshare` | Stop LAN sharing for the current site and release its port |

The proxy runs inside the lerd daemon (`lerd-ui`) — no external tool needed and no internet access required. Any device on the same network can reach the site at `http://<your-LAN-IP>:<port>` without configuring DNS. The assigned port is stored in `sites.yaml` and reused across restarts. The proxy rewrites the Host header so nginx routes correctly, and rewrites absolute URLs in HTML/CSS/JS responses so asset and redirect URLs point to the LAN address instead of the `.test` domain. See [LAN sharing](/usage/remote-development#lan-sharing-per-site) for details.

`lerd share` (without `lan:`) is different — it wraps an external tunnel tool (ngrok/cloudflared/Expose/SSH) to expose the site to the **public internet**.

### Full LAN exposure (all sites, DNS-based)

| Command | Description |
|---|---|
| `lerd lan:expose` | Expose all lerd services to the LAN — binds nginx to `0.0.0.0`, starts the DNS forwarder |
| `lerd lan:unexpose` | Restrict everything back to `127.0.0.1` |
| `lerd lan:status` | Show whether lerd is currently exposed to the local network |

See [Remote / LAN Development](/usage/remote-development) for the full walkthrough.

## PHP

| Command | Description |
|---|---|
| `lerd use <version>` | Set the global PHP version and build the FPM image if needed |
| `lerd isolate <version>` | Pin PHP version for cwd — writes `.php-version` and updates `.lerd.yaml` if present, then re-links |
| `lerd php:list` | List all installed PHP-FPM versions |
| `lerd php:rebuild [--local]` | Force-rebuild all installed PHP-FPM images (pulls pre-built base by default; `--local` builds from source) |
| `lerd fetch [version...] [--local]` | Pull pre-built PHP FPM base images from ghcr.io for the given (or all supported) versions; `--local` builds from source instead |
| `lerd xdebug on [version]` | Enable Xdebug for a PHP version |
| `lerd xdebug off [version]` | Disable Xdebug |
| `lerd xdebug status` | Show Xdebug enabled/disabled for all installed PHP versions |
| `lerd php:ext add <ext> [version]` | Add a custom PHP extension and rebuild the FPM image |
| `lerd php:ext remove <ext> [version]` | Remove a custom PHP extension and rebuild |
| `lerd php:ext list [version]` | List custom extensions for a PHP version |
| `lerd php:ini [version]` | Open the user php.ini for a PHP version in `$EDITOR` |

## Node

| Command | Description |
|---|---|
| `lerd node:install <version>` | Install a Node.js version globally via fnm |
| `lerd node:uninstall <version>` | Uninstall a Node.js version via fnm |
| `lerd node:use <version>` | Set the default Node.js version |
| `lerd isolate:node <version>` | Pin Node version for cwd — writes `.node-version`, runs `fnm install` |
| `lerd node [args...]` | Run `node` using the project's pinned version via fnm |
| `lerd npm [args...]` | Run `npm` using the project's pinned Node version via fnm |
| `lerd npx [args...]` | Run `npx` using the project's pinned Node version via fnm |

## Services

| Command | Description |
|---|---|
| `lerd service start <name>` | Start a service (auto-installs on first use) |
| `lerd service stop <name>` | Stop a service container |
| `lerd service restart <name>` | Restart a service container |
| `lerd service status <name>` | Show systemd unit status |
| `lerd service list` | Show all services and their current state |
| `lerd service expose <name> <host:container>` | Publish an extra port on a built-in service (persisted, auto-restarts if running) |
| `lerd service expose <name> <host:container> --remove` | Remove a previously exposed port |
| `lerd service pin <name>` | Pin a service so it is never auto-stopped when no sites use it |
| `lerd service unpin <name>` | Unpin a service so it can be auto-stopped when unused |
| `lerd service add [file.yaml]` | Register a new custom service (from a YAML file or flags) |
| `lerd service remove <name>` | Stop and remove a custom service |
| `lerd minio:migrate` | Migrate existing MinIO data to RustFS |

## Database

| Command | Description |
|---|---|
| `lerd db:create [name]` | Create a database and a `<name>_testing` database |
| `lerd db:import [-d name] <file.sql>` | Import a SQL dump (defaults to site DB from `.env`) |
| `lerd db:export [-d name] [-o file.sql]` | Export a database to a SQL dump (defaults to site DB from `.env`) |
| `lerd db:shell` | Open an interactive MySQL or PostgreSQL shell |

## Import

| Command | Description |
|---|---|
| `lerd import sail` | Import database and S3/MinIO files from a Laravel Sail project into lerd |
| `lerd sail import` | Alias — natural order when already in a Sail project (`lerd sail <anything-else>` proxies to `vendor/bin/sail`) |
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

For projects that use `laravel/horizon` — lerd detects it automatically from `composer.json`.

| Command | Description |
|---|---|
| `lerd horizon:start` | Start Laravel Horizon for the current project as a persistent background service |
| `lerd horizon:stop` | Stop Horizon |

## Reverb

Requires [Laravel Broadcasting](https://laravel.com/docs/13.x/broadcasting) with the `laravel/reverb` package — lerd detects it automatically from `composer.json`.

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

## Framework definitions

| Command | Description |
|---|---|
| `lerd framework list` | List all available framework definitions and their workers |
| `lerd framework add <name>` | Add or update a framework definition (flags or `--from-file`) |
| `lerd framework remove <name>` | Remove a user-defined framework definition |

## Stripe

| Command | Description |
|---|---|
| `lerd stripe:listen` | Start a Stripe webhook listener for the current project as a background service |
| `lerd stripe:listen stop` | Stop the Stripe webhook listener |

## Console & runtime passthrough

| Command | Description |
|---|---|
| `lerd console [args...]` | Run the framework's console command (e.g., `php artisan` for Laravel, `php bin/console` for Symfony) inside the project's PHP-FPM container |
| `lerd artisan [args...]` | Alias for `lerd console` — equivalent to `php artisan` since the `php` shim also runs inside the FPM container |
| `lerd a [args...]` | Short alias for `lerd console` / `lerd artisan` |
| `lerd test [args...]` | Shortcut for `lerd artisan test` |
| `lerd <vendor-bin> [args...]` | Run any composer-installed binary from the project's `vendor/bin` directory (e.g. `lerd pest`, `lerd pint`, `lerd phpstan`). Real lerd commands always win over vendor binaries with the same name. |
| `lerd shell` | Open an interactive shell inside the project's PHP-FPM container |

## AI integration

| Command | Description |
|---|---|
| `lerd mcp:enable-global` | Register lerd MCP at user scope — available in every Claude Code session regardless of directory |
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
