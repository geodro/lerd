# AI Integration (MCP)

Lerd ships a [Model Context Protocol](https://modelcontextprotocol.io/) server, letting AI assistants (Claude Code, JetBrains Junie, and any other MCP-compatible tool) manage your dev environment directly — run migrations, start services, toggle queue workers, and inspect logs without leaving the chat.

---

## Setting up MCP

There are two ways to connect lerd to your AI assistant: globally (recommended) or per-project.

### Global registration (recommended)

Run once after installing lerd:

```bash
lerd mcp:enable-global
```

This registers the lerd MCP server at **user scope** — available in every Claude Code session, regardless of which directory you open. It also updates Windsurf and JetBrains Junie global configs.

When running globally, the server uses the **directory Claude is opened in** as the site context. No further configuration is needed — just open your AI assistant in a project directory and lerd tools work immediately.

> **During `lerd install`:** If Claude Code is detected, you'll be prompted to run this automatically.

### Project-scoped registration

To pin lerd to a specific project path (useful for teams or when sharing config via git):

```bash
cd ~/Lerd/my-app
lerd mcp:inject
```

This writes five files into the project directory:

| File | Purpose |
|---|---|
| `.mcp.json` | MCP server entry for Claude Code |
| `.claude/skills/lerd/SKILL.md` | Skill file that teaches Claude about lerd tools |
| `.ai/mcp/mcp.json` | MCP server entry for Windsurf and other MCP-compatible tools |
| `.junie/mcp/mcp.json` | MCP server entry for JetBrains Junie |
| `.junie/guidelines.md` | Lerd context section for JetBrains Junie (merged, not overwritten) |

The config includes a `LERD_SITE_PATH` environment variable pointing to the project root, which takes precedence over the cwd fallback.

The command **merges** into existing configs — other MCP servers (e.g. `laravel-boost`, `herd`) are left untouched. Re-running it is safe.

To target a different directory:

```bash
lerd mcp:inject --path ~/Lerd/another-app
```

### Path resolution

Tools like `artisan`, `composer`, `env_setup`, and `db_export` accept an optional `path` argument. When omitted, the server resolves the path in this order:

1. Explicit `path` argument (highest priority)
2. `LERD_SITE_PATH` env var (set by `mcp:inject`)
3. Current working directory — the directory Claude was opened in (global sessions)

---

## Available MCP tools

Once the MCP server is connected, your AI assistant has access to:

| Tool | Description |
|---|---|
| `sites` | List all registered lerd sites (name, domain, path, PHP/Node version, framework, worker status) |
| `runtime_versions` | List installed PHP and Node.js versions with configured defaults |
| `artisan` | Run `php artisan` in the PHP-FPM container — migrations, generators, seeders, cache, tinker |
| `composer` | Run `composer` in the PHP-FPM container — install, require, dump-autoload, etc. |
| `node_install` | Install a Node.js version via fnm (e.g. `"20"`, `"lts"`) |
| `node_uninstall` | Uninstall a Node.js version via fnm |
| `env_setup` | Configure `.env` for lerd: detects services, starts them, creates DB, sets APP_KEY and APP_URL |
| `site_link` | Register a directory as a lerd site — generates nginx vhost and `.test` domain |
| `site_unlink` | Unregister a site and remove its nginx vhost |
| `secure` | Enable HTTPS for a site using a locally-trusted mkcert certificate |
| `unsecure` | Disable HTTPS for a site |
| `xdebug_on` | Enable Xdebug for a PHP version and restart the FPM container |
| `xdebug_off` | Disable Xdebug for a PHP version |
| `xdebug_status` | Show Xdebug enabled/disabled state for all PHP versions |
| `service_start` | Start a built-in or custom service; if the service has `depends_on`, dependencies start first and dependent services start after |
| `service_stop` | Stop a built-in or custom service; cascade-stops any custom services that depend on it first |
| `service_add` | Register a new custom OCI-based service (MongoDB, RabbitMQ, …); supports `depends_on` for service dependencies |
| `service_remove` | Stop and deregister a custom service |
| `service_expose` | Add or remove an extra published port on a built-in service (persisted, auto-restarts if running) |
| `service_env` | Return the recommended `.env` connection variables for a built-in or custom service |
| `db_export` | Export a database to a SQL dump file (defaults to site DB from `.env`) |
| `queue_start` | Start the queue worker for a site (any framework with a `queue` worker) |
| `queue_stop` | Stop the queue worker |
| `horizon_start` | Start Laravel Horizon for a site (use instead of `queue_start` when `laravel/horizon` is installed) |
| `horizon_stop` | Stop Laravel Horizon |
| `reverb_start` | Start the Reverb WebSocket server for a site |
| `reverb_stop` | Stop the Reverb server |
| `schedule_start` | Start the task scheduler for a site |
| `schedule_stop` | Stop the task scheduler |
| `worker_start` | Start any named framework worker (e.g. `messenger`, `pulse`) |
| `worker_stop` | Stop a named framework worker |
| `worker_list` | List all workers defined for a site's framework with running status |
| `framework_list` | List all framework definitions including their workers |
| `framework_add` | Add or update a framework definition; use `name: "laravel"` to add custom workers to Laravel |
| `framework_remove` | Remove a user-defined framework; for `laravel` removes only custom worker additions |
| `site_php` | Change the PHP version for a registered site — writes `.php-version`, updates registry, regenerates nginx vhost |
| `site_node` | Change the Node.js version for a registered site — writes `.node-version`, installs via fnm if needed |
| `site_pause` | Pause a site: stop all its workers and replace its vhost with a landing page |
| `site_unpause` | Resume a paused site: restore its vhost and restart previously running workers |
| `service_pin` | Pin a service so it is never auto-stopped even when no sites reference it |
| `service_unpin` | Unpin a service so it can be auto-stopped when unused |
| `stripe_listen` | Start a Stripe webhook listener for a site (reads `STRIPE_SECRET` from `.env`) |
| `stripe_listen_stop` | Stop the Stripe webhook listener |
| `logs` | Fetch container logs — defaults to current site's FPM; optionally specify nginx, service name, PHP version, or site name |
| `status` | Health snapshot of DNS, nginx, PHP-FPM containers, and the watcher — use when a site isn't loading |
| `doctor` | Full diagnostic: podman, systemd, DNS, ports, PHP images, config, updates — use when the user reports setup issues |

---

## Example interactions

The `path` argument is omitted from most calls — the server resolves it from the directory Claude was opened in (global sessions) or from `LERD_SITE_PATH` (project-scoped sessions).

```
You: create a new Laravel project and get it running
AI:  → composer(args: ["create-project", "laravel/laravel", "."])
     → site_link()
     → env_setup()
       # detects MySQL + Redis, starts them, creates database, generates APP_KEY
     → artisan(args: ["migrate"])
     ✓  myapp -> myapp.test ready

You: run migrations
AI:  → artisan(args: ["migrate"])
     ✓  Ran 3 migrations in 42ms

You: install sanctum and run its migrations
AI:  → composer(args: ["require", "laravel/sanctum"])
     → artisan(args: ["vendor:publish", "--provider=Laravel\\Sanctum\\SanctumServiceProvider"])
     → artisan(args: ["migrate"])

You: add a MongoDB service
AI:  → service_add(name: "mongodb", image: "docker.io/library/mongo:7", ports: ["27017:27017"], data_dir: "/data/db")
     → service_start(name: "mongodb")
     ✓  mongodb started

You: add phpMyAdmin, it needs MySQL to be running
AI:  → service_add(name: "phpmyadmin", image: "docker.io/phpmyadmin:latest", ports: ["8080:80"], depends_on: ["mysql"], dashboard: "http://localhost:8080")
     → service_start(name: "phpmyadmin")
       # starts mysql first (dependency), then phpmyadmin
     ✓  mysql started
     ✓  phpmyadmin started

You: what PHP and Node versions are installed?
AI:  → runtime_versions()
     { "php": { "installed": ["8.3", "8.4"], "default_version": "8.4" },
       "node": { "installed": ["v20.11.0", "v18.20.4"], "default_version": "20" } }

You: set up the project I just cloned
AI:  → site_link()
     → env_setup()
       # detects MySQL + Redis, starts them, creates database, generates APP_KEY
     → composer(args: ["install"])
     → artisan(args: ["migrate", "--seed"])
     ✓  whitewaters -> whitewaters.test ready

You: enable xdebug so I can step through a failing job
AI:  → xdebug_status()
     → xdebug_on(version: "8.4")
     ✓  Xdebug enabled for PHP 8.4 (port 9003)

You: the app is throwing 500s — check the logs
AI:  → logs(target: "8.4", lines: 50)
     PHP Fatal error: Class "App\Jobs\ProcessOrder" not found ...
```
