# Command Reference

## Setup & lifecycle

| Command | Description |
|---|---|
| `lerd install` | One-time setup: directories, network, binaries, DNS, nginx, watcher |
| `lerd start` | Start DNS, nginx, PHP-FPM containers, and all installed services |
| `lerd stop` | Stop DNS, nginx, PHP-FPM containers, and all running services |
| `lerd quit` | Stop all Lerd processes and containers including the UI, watcher, and tray |
| `lerd update` | Check for updates, show changelog, and update after confirmation |
| `lerd uninstall` | Stop all containers and remove Lerd |
| `lerd uninstall --force` | Same, skipping all confirmation prompts |
| `lerd autostart enable` | Start Lerd automatically on every login |
| `lerd autostart disable` | Disable autostart on login |
| `lerd tray` | Launch the system tray applet (detaches from terminal) |
| `lerd autostart tray enable` | Start the tray applet automatically on graphical login |
| `lerd autostart tray disable` | Disable tray autostart |
| `lerd dns:check` | Verify that `*.test` resolves to `127.0.0.1` |
| `lerd status` | Health summary: DNS, nginx, PHP-FPM containers, watcher, services, cert expiry |
| `lerd about` | Show version, build info, and project URL |
| `lerd man [page]` | Browse the built-in documentation in the terminal; pass a page name to jump directly (e.g. `lerd man sites`) |
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
| `lerd setup` | Interactive project bootstrap — checkbox list of steps to run in sequence |
| `lerd setup --all` | Run all setup steps without prompting (useful in CI) |
| `lerd setup --skip-open` | Same as above but don't open the browser at the end |

## Site management

| Command | Description |
|---|---|
| `lerd park [dir]` | Register all Laravel projects inside `dir` (defaults to cwd) |
| `lerd unpark [dir]` | Remove a parked directory and unlink all its sites |
| `lerd link [name]` | Register the current directory as a site |
| `lerd link [name] --domain foo.test` | Register with a custom domain |
| `lerd unlink [name]` | Stop serving the site |
| `lerd sites` | Table view of all registered sites |
| `lerd open [name]` | Open the site in the default browser |
| `lerd share [name]` | Expose the site publicly via ngrok or Expose (auto-detected) |
| `lerd secure [name]` | Issue a mkcert TLS cert and enable HTTPS — updates `APP_URL` in `.env` |
| `lerd unsecure [name]` | Remove TLS and switch back to HTTP — updates `APP_URL` in `.env` |
| `lerd pause [name]` | Pause a site: stop its workers and replace the vhost with a landing page |
| `lerd unpause [name]` | Resume a paused site: restore its vhost and restart previously running workers |
| `lerd env` | Configure `.env` for the current project with lerd service connection settings |

## PHP

| Command | Description |
|---|---|
| `lerd use <version>` | Set the global PHP version and build the FPM image if needed |
| `lerd isolate <version>` | Pin PHP version for cwd — writes `.php-version` |
| `lerd php:list` | List all installed PHP-FPM versions |
| `lerd php:rebuild` | Force-rebuild all installed PHP-FPM images |
| `lerd fetch [version...]` | Pre-build PHP FPM images for the given (or all supported) versions |
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
| `lerd artisan [args...]` | Alias for `lerd console` (Laravel-specific, kept for backward compatibility) |
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
