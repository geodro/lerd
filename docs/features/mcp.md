# AI Integration (MCP)

Lerd ships a [Model Context Protocol](https://modelcontextprotocol.io/) server, letting AI assistants manage your dev environment directly: run migrations, start services, toggle queue workers, and inspect logs without leaving the chat.

Supported assistants: **Claude Code, Cursor, JetBrains Junie, Codex CLI, Gemini CLI, GitHub Copilot (VS Code), Google Antigravity, Windsurf**, and any other MCP-compatible tool.

---

## Setting up MCP

There are two ways to connect lerd to your AI assistant: globally (recommended) or per-project.

### Global registration (recommended)

Run once after installing lerd:

```bash
lerd mcp:enable-global
```

This registers the lerd MCP server at **user scope**, available in every session regardless of which directory you open, and writes user-scope context files so the assistant knows what lerd tools are available and how to use them.

MCP server registration:

| Client | Registration |
|---|---|
| Claude Code | `claude mcp add --scope user` (CLI) |
| Cursor | `~/.cursor/mcp.json` |
| Windsurf | `~/.ai/mcp/mcp.json` |
| JetBrains Junie | `~/.junie/mcp/mcp.json` |
| Gemini CLI | `~/.gemini/settings.json` |
| Codex CLI | `~/.codex/config.toml` |
| GitHub Copilot (VS Code) | `~/.config/Code/User/mcp.json` |
| Google Antigravity | `~/.gemini/config/mcp_config.json` |

Context / instructions files:

| File | Purpose |
|---|---|
| `~/.claude/skills/lerd/SKILL.md` | Claude Code user-scope skill |
| `~/.cursor/rules/lerd.mdc` | Cursor user-scope rules |
| `~/.junie/guidelines.md` | JetBrains Junie user-scope guidelines (merged, not overwritten) |
| `~/.gemini/GEMINI.md` | Gemini CLI user-scope context (merged) |
| `~/.codex/AGENTS.md` | Codex CLI user-scope context (merged) |

All clients share a single canonical tool reference, so the guidance never drifts between assistants. When running globally, the server uses the **directory the assistant is opened in** as the site context: no further configuration is needed.

> **Claude Code** is registered via its own `claude mcp add` CLI rather than by editing `~/.claude.json` directly, since that file holds all of Claude's user state.

> **GitHub Copilot** uses VS Code's `servers` key (each entry typed `stdio`), which differs from the `mcpServers` key the other clients use. Its instructions file (`.github/copilot-instructions.md`) is project-scoped only.

> **Google Antigravity** registers at `~/.gemini/config/mcp_config.json` (its project-scoped MCP config is not honoured, so it is global only). It auto-loads `GEMINI.md` and `AGENTS.md`, which the Gemini and Codex entries already write, so no separate Antigravity context file is needed.

> **During `lerd install`:** If Claude Code is detected, you'll be prompted to run this automatically.

> **During `lerd update`:** When MCP is globally registered, the context files that already exist are rewritten from the newly installed binary so they stay in sync with any added or renamed tools. Update never creates files for a client you haven't set up — to pick up a newly supported assistant, re-run `lerd mcp:enable-global`.

### Project-scoped registration

To pin lerd to a specific project path (useful for teams or when sharing config via git):

```bash
cd ~/Lerd/my-app
lerd mcp:inject
```

This writes MCP config and context files for every supported client into the project directory:

| File | Client |
|---|---|
| `.mcp.json` | Claude Code MCP config |
| `.claude/skills/lerd/SKILL.md` | Claude Code skill |
| `.cursor/mcp.json` | Cursor MCP config |
| `.cursor/rules/lerd.mdc` | Cursor rules |
| `.ai/mcp/mcp.json` | Windsurf MCP config |
| `.junie/mcp/mcp.json` | JetBrains Junie MCP config |
| `.junie/guidelines.md` | JetBrains Junie guidelines (merged) |
| `.gemini/settings.json` | Gemini CLI MCP config |
| `GEMINI.md` | Gemini CLI context (merged) |
| `.vscode/mcp.json` | GitHub Copilot (VS Code) MCP config |
| `.github/copilot-instructions.md` | GitHub Copilot instructions (merged) |
| `AGENTS.md` | Codex CLI context (merged) |

The MCP config includes a `LERD_SITE_PATH` environment variable pointing to the project root, which takes precedence over the cwd fallback.

> **Codex** has no project-scoped MCP config (it reads only `~/.codex/config.toml`), so `mcp:inject` writes its `AGENTS.md` context but not a per-project server entry. Register Codex once with `lerd mcp:enable-global`.

The command **merges** into existing configs; other MCP servers (e.g. `laravel-boost`, `herd`) and any existing instructions content are left untouched. Re-running it is safe.

To target a different directory:

```bash
lerd mcp:inject --path ~/Lerd/another-app
```

> **During `lerd update`:** Projects that previously ran `mcp:inject` are detected automatically (by the presence of `.claude/skills/lerd/SKILL.md`, `.cursor/rules/lerd.mdc`, or the lerd marker in `.junie/guidelines.md`) and refreshed in place. Only files that already exist for a client are rewritten, so update never drops new client files into your repo; re-run `mcp:inject` to add a newly supported assistant. Directories whose content already matches the new binary stay untouched, so git status stays clean. Projects that never opted in are skipped.

### Path resolution

Most actions accept an optional `path` argument. When omitted, the server resolves it in this order:

1. Explicit `path` argument (highest priority)
2. `LERD_SITE_PATH` env var (set by `mcp:inject`)
3. Current working directory, the directory the assistant was opened in (global sessions)

### Agent detection passthrough

PHP runs inside the container, so the AI agent environment variables your coding agent exports on the host (`CLAUDECODE`, `AI_AGENT`, `CURSOR_AGENT`, `GEMINI_CLI`, and the rest of the [agent-detector](https://github.com/laravel/agent-detector) set) would normally be lost at the container boundary. lerd forwards any that are present into the container for `lerd php`, `lerd artisan`, and tinker, so packages like [laravel/pao](https://github.com/laravel/pao) still detect the agent and emit their compact JSON output.

Commands the `exec` MCP tool runs (`artisan`, `composer`, `vendor_run` for Pest/PHPUnit) go a step further: because reaching them through the MCP server proves an agent is driving the command, lerd injects a neutral `AI_AGENT=lerd-mcp` marker when no real agent variable is present. Pao therefore returns JSON for MCP-originated test runs even if the host environment carries nothing, while a real agent variable is still forwarded as-is when it exists. Manual terminal runs are never given the marker, so their output is unchanged.

---

## Available MCP tools

The MCP surface is **eleven grouped tools**, each driven by an `action` argument. Always pass `action`; start by calling `site` with `action: "list"` to discover sites.

| Tool | Actions |
|---|---|
| `site` | `list` (discover sites — call first), `link`, `unlink`, `domain_add`, `domain_remove`, `group_assign`, `group_unassign`, `group_label`, `group_db`, `group_list`, `tls_enable`, `tls_disable`, `php`, `node`, `pause`, `unpause`, `restart`, `rebuild`, `runtime`, `nginx_read`, `nginx_write`, `nginx_reset`, `park`, `unpark` |
| `service` | `start`, `stop`, `restart`, `pin`, `unpin`, `update`, `rollback`, `migrate`, `remove`, `reinstall`, `add`, `expose`, `port`, `env`, `config_read`, `config_write`, `config_restore`, `config_reset`, `config_list_backups`, `preset_list`, `preset_install`, `check_updates` |
| `db` | `set`, `move`, `create`, `export`, `import`, `snapshot`, `snapshots`, `restore`, `snapshot_delete` |
| `env` | `setup`, `check`, `override` |
| `runtime` | `versions`, `node_install`, `node_uninstall`, `php_list`, `ext_list`, `ext_add`, `ext_remove` |
| `worker` | `list` (call first), `start`, `stop`, `add`, `remove`, `health`, `heal`, `mode_get`, `mode_set`, `queue_start`, `queue_stop`, `horizon_start`, `horizon_stop`, `reverb_start`, `reverb_stop`, `schedule_start`, `schedule_stop`, `stripe_start`, `stripe_stop`, `stripe_config` |
| `exec` | `artisan`, `console`, `composer`, `vendor_bins`, `vendor_run`, `commands_list`, `commands_run`, `command_add`, `command_remove` |
| `framework` | `list`, `add`, `remove`, `search`, `install`, `project_new`, `setup` |
| `diag` | `status`, `doctor`, `site_doctor`, `which`, `check`, `dns_diagnose`, `bug_report`, `analyze_queries`, `dumps_recent`, `dumps_status`, `dumps_clear`, `dumps_toggle`, `profiler_toggle`, `profiler_status`, `profiler_clear`, `xdebug_on`, `xdebug_off`, `xdebug_status` |
| `logs` | `sources`, `fetch` |
| `worktree` | `list`, `add`, `remove`, `db_isolate`, `db_share` |

The injected context files document each action's arguments and the key conventions in full.

### Reading logs

The `logs` tool lets an assistant debug a site's logs without opening files by hand. Call `logs` with `action: "sources"` to list every queryable source for a site (`app:<file>` framework logs, `fpm`, `worker:<name>`) plus shared infrastructure (`nginx`, `dns`, `watcher`, `ui`, services, `php<ver>`), then `action: "fetch"` with a `source` and any of `grep` (regex or literal substring), `since`/`until` (relative like `15m`/`2h30m`, or a timestamp), `level` (app logs only), and `lines`. Each `fetch` returns an opaque `cursor`; pass it back as `since` on the next call to receive only the new lines, which is how streaming is modelled over MCP's request/response transport. See [the logs feature page](logs.md) for the full source list, filter semantics, and platform notes.

> **Grouped surface:** earlier lerd versions exposed ~80 individual MCP tools (`sites`, `artisan`, `db_set`, …). These were consolidated into the grouped tools above to cut the per-session token cost and sharpen the model's tool selection. Old flat tool names no longer exist; call the group with the matching `action` instead (e.g. `artisan` → `exec` with `action: "artisan"`, `db_set` → `db` with `action: "set"`).

---

## Example interactions

The `path` argument is omitted from most calls; the server resolves it from the directory the assistant was opened in (global sessions) or from `LERD_SITE_PATH` (project-scoped sessions).

```
You: create a new Laravel project and get it running
AI:  → framework(action: "project_new", path: "/home/me/Code/myapp", framework: "laravel")
       # scaffolds + runs composer install, returns with vendor/ populated
     → site(action: "link", path: "/home/me/Code/myapp")
     → env(action: "setup", path: "/home/me/Code/myapp")
       # detects MySQL + Redis (or keeps sqlite), starts services, creates DB, generates APP_KEY
     → framework(action: "setup", path: "/home/me/Code/myapp")
       # runs storage:link + migrate
     ✓  myapp -> myapp.test ready

You: run migrations
AI:  → exec(action: "artisan", args: ["migrate"])
     ✓  Ran 3 migrations in 42ms

You: install sanctum and run its migrations
AI:  → exec(action: "composer", args: ["require", "laravel/sanctum"])
     → exec(action: "artisan", args: ["vendor:publish", "--provider=Laravel\\Sanctum\\SanctumServiceProvider"])
     → exec(action: "artisan", args: ["migrate"])

You: add a MongoDB service
AI:  → service(action: "add", name: "mongodb", image: "docker.io/library/mongo:7", ports: ["27017:27017"], data_dir: "/data/db")
     → service(action: "start", name: "mongodb")
     ✓  mongodb started

You: add phpMyAdmin, it needs MySQL to be running
AI:  → service(action: "preset_install", name: "phpmyadmin")
     → service(action: "start", name: "phpmyadmin")
       # starts mysql first (dependency), then phpmyadmin
     ✓  mysql started
     ✓  phpmyadmin started

You: what PHP and Node versions are installed?
AI:  → runtime(action: "versions")
     { "php": { "installed": ["8.4", "8.5"], "default_version": "8.5" },
       "node": { "installed": ["v20.11.0", "v18.20.4"], "default_version": "20" } }

You: set up the project I just cloned
AI:  → site(action: "link")
     → exec(action: "composer", args: ["install"])
       # runs BEFORE env setup so APP_KEY generation has vendor/
     → env(action: "setup")
       # detects MySQL + Redis, starts them, creates database, generates APP_KEY
     → framework(action: "setup")
       # framework migrations + storage:link (or doctrine:migrations:migrate for Symfony)
     ✓  whitewaters -> whitewaters.test ready

You: enable xdebug so I can step through a failing job
AI:  → diag(action: "xdebug_status")
     → diag(action: "xdebug_on", version: "8.5")
     ✓  Xdebug enabled for PHP 8.5 (mode=debug, port 9003)

You: start the queue worker
AI:  → worker(action: "list", site: "myapp")
     → worker(action: "queue_start", site: "myapp")
     ✓  queue worker started for myapp

You: the app is throwing 500s, check the logs
AI:  → logs(action: "sources", site: "myapp")
     → logs(action: "fetch", source: "app:laravel.log", level: "error", since: "15m")
     PHP Fatal error: Class "App\Jobs\ProcessOrder" not found ...
```
