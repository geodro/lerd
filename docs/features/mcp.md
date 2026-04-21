# AI Integration (MCP)

Lerd ships a [Model Context Protocol](https://modelcontextprotocol.io/) server, letting AI assistants (Claude Code, Cursor, JetBrains Junie, and any other MCP-compatible tool) manage your dev environment directly: run migrations, start services, toggle queue workers, and inspect logs without leaving the chat.

---

## Setting up MCP

There are two ways to connect lerd to your AI assistant: globally (recommended) or per-project.

### Global registration (recommended)

Run once after installing lerd:

```bash
lerd mcp:enable-global
```

This registers the lerd MCP server at **user scope**, available in every Claude Code session, regardless of which directory you open. It also updates Cursor, Windsurf, and JetBrains Junie global configs, and writes user-scope skill, rules, and guidelines files so the assistant knows what lerd tools are available and how to use them:

| File | Purpose |
|---|---|
| `~/.claude/skills/lerd/SKILL.md` | Claude Code user-scope skill |
| `~/.cursor/rules/lerd.mdc` | Cursor user-scope rules |
| `~/.junie/guidelines.md` | JetBrains Junie user-scope guidelines (merged, not overwritten) |

When running globally, the server uses the **directory Claude is opened in** as the site context. No further configuration is needed: just open your AI assistant in a project directory and lerd tools work immediately.

> **During `lerd install`:** If Claude Code is detected, you'll be prompted to run this automatically.

> **During `lerd update`:** When MCP is globally registered, the skill, rules, and guidelines files are automatically rewritten from the newly installed binary so they stay in sync with any added or renamed tools.

### Project-scoped registration

To pin lerd to a specific project path (useful for teams or when sharing config via git):

```bash
cd ~/Lerd/my-app
lerd mcp:inject
```

This writes seven files into the project directory:

| File | Purpose |
|---|---|
| `.mcp.json` | MCP server entry for Claude Code |
| `.claude/skills/lerd/SKILL.md` | Skill file that teaches Claude about lerd tools |
| `.cursor/mcp.json` | MCP server entry for Cursor |
| `.cursor/rules/lerd.mdc` | Cursor rules file that teaches Cursor about lerd tools |
| `.ai/mcp/mcp.json` | MCP server entry for Windsurf and other MCP-compatible tools |
| `.junie/mcp/mcp.json` | MCP server entry for JetBrains Junie |
| `.junie/guidelines.md` | Lerd context section for JetBrains Junie (merged, not overwritten) |

The config includes a `LERD_SITE_PATH` environment variable pointing to the project root, which takes precedence over the cwd fallback.

The command **merges** into existing configs; other MCP servers (e.g. `laravel-boost`, `herd`) are left untouched. Re-running it is safe.

To target a different directory:

```bash
lerd mcp:inject --path ~/Lerd/another-app
```

> **During `lerd update`:** Projects that previously ran `mcp:inject` are detected automatically (by the presence of `.claude/skills/lerd/SKILL.md`, `.cursor/rules/lerd.mdc`, or the lerd marker in `.junie/guidelines.md`) and their per-project artefacts are refreshed in place. Directories whose content already matches the new binary stay untouched, so git status stays clean. Projects that never opted in are skipped.

### Path resolution

Tools like `artisan`, `composer`, `env_setup`, `env_check`, `db_export`, `db_import`, and `db_create` accept an optional `path` argument. When omitted, the server resolves the path in this order:

1. Explicit `path` argument (highest priority)
2. `LERD_SITE_PATH` env var (set by `mcp:inject`)
3. Current working directory, the directory Claude was opened in (global sessions)

---

## Available MCP tools

Once the MCP server is connected, your AI assistant has access to:

| Tool | Description |
|---|---|
| `sites` | List all registered lerd sites (name, domain, path, PHP/Node version, framework, worker status) |
| `runtime_versions` | List installed PHP and Node.js versions with configured defaults |
| `php_list` | List all PHP versions installed by lerd, marking the global default |
| `php_ext` | Manage custom PHP extensions for a PHP version — `action`: `list` / `add` / `remove` (`add` and `remove` rebuild the FPM image and restart the container) |
| `artisan` | Run `php artisan` in the PHP-FPM container: migrations, generators, seeders, cache, tinker (Laravel only) |
| `console` | Run the framework's console command (e.g. `php bin/console` for Symfony); shown for non-Laravel frameworks that define a `console` field |
| `composer` | Run `composer` in the PHP-FPM container: install, require, dump-autoload, etc. |
| `node` | Install or uninstall a Node.js version via fnm — `action`: `install` / `uninstall` (e.g. `"20"`, `"lts"`) |
| `env_setup` | Configure `.env` for lerd: detects services, starts them, creates DB, sets APP_KEY and APP_URL |
| `env_check` | Compare all `.env` files against `.env.example` and flag missing or extra keys (returns structured JSON) |
| `site_link` | Register a directory as a lerd site; generates nginx vhost and `.test` domain |
| `site_unlink` | Unregister a site and remove its nginx vhost (all domains) |
| `site_domain` | Add or remove a site domain (without TLD) — `action`: `add` / `remove`; cannot remove last |
| `park` | Register a parent directory; scans subdirectories and auto-registers any PHP projects as sites |
| `unpark` | Remove a parked directory from lerd and unlink all its sites |
| `site_tls` | Enable or disable HTTPS for a site using a locally-trusted mkcert certificate — `action`: `enable` / `disable` |
| `xdebug` | Manage Xdebug for a PHP version — `action`: `on` / `off` / `status`. `on` accepts optional `mode` (default `debug`; accepts `coverage`, `develop`, `profile`, `trace`, `gcstats`, or comma combos like `debug,coverage`); `status` reports state and active mode for all PHP versions |
| `service_control` | Start, stop, pin, or unpin a built-in or custom service — `action`: `start` / `stop` / `pin` / `unpin`. Starting/stopping respects `depends_on` cascades; `pin` keeps a service running regardless of site activity |
| `service_add` | Register a new custom OCI-based service (MongoDB, RabbitMQ, etc.); supports `depends_on` for service dependencies |
| `service_remove` | Stop and deregister a custom service |
| `service_expose` | Add or remove an extra published port on a built-in service (persisted, auto-restarts if running) |
| `service_env` | Return the recommended `.env` connection variables for a built-in or custom service |
| `db_export` | Export a database to a SQL dump file (defaults to site DB from `.env`) |
| `db_import` | Import a SQL dump file into the project database (reads connection from `.env`) |
| `db_create` | Create a database and `_testing` variant for the project (infers name from `.env` or project dir) |
| `queue` | Start or stop the queue worker for a site — `action`: `start` / `stop` (any framework with a `queue` worker) |
| `horizon` | Start or stop Laravel Horizon for a site — `action`: `start` / `stop` (use instead of `queue` when `laravel/horizon` is installed) |
| `reverb` | Start or stop the Reverb WebSocket server for a site — `action`: `start` / `stop` |
| `schedule` | Start or stop the task scheduler for a site — `action`: `start` / `stop` |
| `worker` | Start or stop any named framework worker (e.g. `messenger`, `pulse`) — `action`: `start` / `stop` |
| `worker_list` | List all workers defined for a site's framework with running status |
| `framework_list` | List all framework definitions including their workers |
| `framework_add` | Add or update a framework definition; use `name: "laravel"` to add custom workers to Laravel |
| `framework_remove` | Remove a user-defined framework; for `laravel` removes only custom worker additions |
| `site_php` | Change the PHP version for a registered site: writes `.php-version`, updates registry, regenerates nginx vhost |
| `site_node` | Change the Node.js version for a registered site: writes `.node-version`, installs via fnm if needed |
| `site_control` | Pause, unpause, restart, or rebuild a site — `action`: `pause` / `unpause` / `restart` / `rebuild` (pause replaces vhost with landing page; rebuild only for custom containers) |
| `stripe` | Start or stop a Stripe webhook listener for a site — `action`: `start` / `stop` (reads `STRIPE_SECRET` from `.env` on start) |
| `logs` | Fetch container logs; defaults to current site's FPM; optionally specify nginx, service name, PHP version, or site name |
| `status` | Health snapshot of DNS, nginx, PHP-FPM containers, and the watcher; use when a site isn't loading |
| `doctor` | Full diagnostic as structured JSON: podman, systemd, DNS, ports, PHP images, config, updates; use when the user reports setup issues |
| `which` | Show the resolved PHP version, Node version, document root, and nginx config for the current site |
| `check` | Validate `.lerd.yaml` as structured JSON (PHP version, services, framework); returns valid/errors/warnings with per-field status |

---

## Example interactions

The `path` argument is omitted from most calls; the server resolves it from the directory Claude was opened in (global sessions) or from `LERD_SITE_PATH` (project-scoped sessions).

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
     → service_control(action: "start", name: "mongodb")
     ✓  mongodb started

You: add phpMyAdmin, it needs MySQL to be running
AI:  → service_add(name: "phpmyadmin", image: "docker.io/phpmyadmin:latest", ports: ["8080:80"], depends_on: ["mysql"], dashboard: "http://localhost:8080")
     → service_control(action: "start", name: "phpmyadmin")
       # starts mysql first (dependency), then phpmyadmin
     ✓  mysql started
     ✓  phpmyadmin started

You: what PHP and Node versions are installed?
AI:  → runtime_versions()
     { "php": { "installed": ["8.4", "8.5"], "default_version": "8.5" },
       "node": { "installed": ["v20.11.0", "v18.20.4"], "default_version": "20" } }

You: set up the project I just cloned
AI:  → site_link()
     → env_setup()
       # detects MySQL + Redis, starts them, creates database, generates APP_KEY
     → composer(args: ["install"])
     → artisan(args: ["migrate", "--seed"])
     ✓  whitewaters -> whitewaters.test ready

You: enable xdebug so I can step through a failing job
AI:  → xdebug(action: "status")
     → xdebug(action: "on", version: "8.5")
     ✓  Xdebug enabled for PHP 8.5 (mode=debug, port 9003)

You: turn on xdebug coverage so I can run phpunit --coverage
AI:  → xdebug(action: "on", version: "8.5", mode: "coverage")
     ✓  Xdebug enabled for PHP 8.5 (mode=coverage, port 9003)

You: the app is throwing 500s, check the logs
AI:  → logs(target: "8.5", lines: 50)
     PHP Fatal error: Class "App\Jobs\ProcessOrder" not found ...
```
