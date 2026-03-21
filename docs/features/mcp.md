# AI Integration (MCP)

Lerd ships a [Model Context Protocol](https://modelcontextprotocol.io/) server, letting AI assistants (Claude Code, JetBrains Junie, and any other MCP-compatible tool) manage your dev environment directly — run migrations, start services, toggle queue workers, and inspect logs without leaving the chat.

---

## Injecting the config

Run this once from a Laravel project root:

```bash
cd ~/Lerd/my-app
lerd mcp:inject
```

This writes five files:

| File | Purpose |
|---|---|
| `.mcp.json` | MCP server entry for Claude Code |
| `.claude/skills/lerd/SKILL.md` | Skill file that teaches Claude about lerd tools |
| `.ai/mcp/mcp.json` | MCP server entry for Windsurf and other MCP-compatible tools |
| `.junie/mcp/mcp.json` | MCP server entry for JetBrains Junie |
| `.junie/guidelines.md` | Lerd context section for JetBrains Junie (merged, not overwritten) |

Each config includes a `LERD_SITE_PATH` environment variable pointing to the project root. This means tools like `artisan`, `composer`, `env_setup`, and `db_export` don't need an explicit `path` argument when called from within that project — the server uses `LERD_SITE_PATH` as the default.

The command **merges** into existing configs — other MCP servers (e.g. `laravel-boost`, `herd`) are left untouched. Re-running it is safe.

To target a different directory:

```bash
lerd mcp:inject --path ~/Lerd/another-app
```

---

## Available MCP tools

Once the MCP server is connected, your AI assistant has access to:

| Tool | Description |
|---|---|
| `sites` | List all registered lerd sites (name, domain, path, PHP/Node version, queue status) |
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
| `service_start` | Start a built-in or custom service |
| `service_stop` | Stop a service |
| `service_add` | Register a new custom OCI-based service (MongoDB, RabbitMQ, …) |
| `service_remove` | Stop and deregister a custom service |
| `db_export` | Export the project database to a SQL dump file |
| `queue_start` | Start a queue worker for a site |
| `queue_stop` | Stop a queue worker |
| `reverb_start` | Start the Laravel Reverb WebSocket server for a site |
| `reverb_stop` | Stop the Reverb server |
| `schedule_start` | Start the Laravel task scheduler for a site |
| `schedule_stop` | Stop the task scheduler |
| `stripe_listen` | Start a Stripe webhook listener for a site (reads `STRIPE_SECRET` from `.env`) |
| `stripe_listen_stop` | Stop the Stripe webhook listener |
| `logs` | Fetch recent container logs (nginx, any service, PHP version, or site name) |

---

## Example interactions

`LERD_SITE_PATH` is set automatically by `mcp:inject`, so `path` is omitted from most calls.

```
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
