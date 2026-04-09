# Laravel walkthrough

End-to-end: from `lerd install` to a Laravel app running on `https://myapp.test` with a database, queue worker, and scheduler.

::: info Prerequisites
You've already run `lerd install` once on this machine. If not, see [Installation](installation.md).
:::

::: tip Drive it from your AI assistant
Run `lerd mcp:enable-global` once and your AI assistant (Claude Code, Junie, Windsurf) can call every command below as an MCP tool — `project_new`, `site_link`, `env_setup`, `db_create`, `secure`, `worker_start`, etc. See [AI Integration](../features/mcp.md).
:::

---

## 1. Create the project

Lerd ships with the official Laravel installer, available in your shell after `lerd install`:

::: code-group

```bash [laravel new]
cd ~/Lerd
laravel new myapp
```

```bash [lerd new]
cd ~/Lerd
lerd new myapp
# runs: composer create-project laravel/laravel ./myapp
```

```bash [existing repo]
cd ~/Lerd
git clone git@github.com:you/myapp.git
```

:::

The `laravel new` installer walks you through starter kit, auth, and database choices interactively. `lerd new` is the framework-agnostic alternative — it runs the bare `composer create-project` so you skip the installer's prompts.

---

## 2. Register the site

```bash
cd myapp
lerd link
```

`lerd link` registers `myapp` and assigns it `http://myapp.test` automatically. No `/etc/hosts` edits — DNS is handled by the lerd dnsmasq container.

::: info Already parked?
If `~/Lerd` was registered with `lerd park ~/Lerd` earlier, every subdirectory under it is auto-linked. You can skip `lerd link` entirely.
:::

---

## 3. Configure PHP, Node, database, services

Run the init wizard from inside the project:

```bash
lerd init
```

```
? PHP version: 8.4
? Node version (leave blank to skip): 22
? Enable HTTPS? Yes
? Database: mysql
? Services: [redis, mailpit]
? Workers to auto-start: [queue, schedule]
Saved .lerd.yaml
```

The wizard writes everything to `.lerd.yaml` in the project root. Commit that file — on any other machine, `lerd link` reads it and restores the same setup without re-running the wizard.

See [Project Setup](../features/project-setup.md) for the full wizard reference.

---

## 4. Bootstrap the project

```bash
lerd setup
```

`lerd setup` reads `.lerd.yaml` and shows a checkbox list, pre-selecting every step that's actually needed:

```
? Select setup steps to run:
  ◉ composer install
  ◉ npm ci
  ◉ lerd env                     # writes DB_*, REDIS_*, MAIL_* into .env
  ◉ php artisan migrate
  ◯ php artisan db:seed
  ◉ php artisan storage:link
  ◉ npm run build
  ◉ lerd secure                  # issues mkcert TLS for myapp.test
  ◉ queue:start
  ◉ schedule:start
  ◉ lerd open
```

Press enter and watch them run. When it's done, the browser opens at `https://myapp.test` and the queue + scheduler are running as systemd user services.

::: info One-shot
`lerd setup --all` skips the prompt and runs every selected step. Useful in scripts or after a fresh clone on CI.
:::

---

## 5. Verify

```bash
lerd status
```

You should see `myapp` listed as `active`, the configured services running, and the queue/schedule workers as `running`. Live logs are in the [Web UI](../features/web-ui.md) at `http://127.0.0.1:7073` under the **App Logs** tab for `myapp`.

---

## What just happened

| Command | What it did |
|---|---|
| `lerd link` | Registered `myapp.test` with nginx + dnsmasq |
| `lerd init` | Wrote `.lerd.yaml` with PHP 8.4, Node 22, MySQL, Redis, Mailpit, queue, schedule |
| `lerd env` (via setup) | Injected `DB_HOST=lerd-mysql`, `REDIS_HOST=lerd-redis`, `MAIL_HOST=lerd-mailpit` into `.env` |
| `lerd db:create` (via env) | Created `myapp` and `myapp_testing` databases |
| `lerd secure` (via setup) | Issued an mkcert cert, switched the vhost to HTTPS, set `APP_URL=https://myapp.test` |
| `lerd worker start queue/schedule` (via setup) | Launched `lerd-queue-myapp` and `lerd-schedule-myapp` systemd units |

---

## Reverb (optional)

If your project uses Laravel Reverb (`composer require laravel/reverb`), a third worker toggle appears automatically in the UI and `lerd setup` step list. Each Reverb-enabled site gets its own `REVERB_SERVER_PORT` starting at `8080`, written to `.env` on first run so multiple Reverb sites can coexist.

```bash
lerd worker start reverb
```

---

## Next steps

- [Frameworks & Workers](../usage/frameworks.md) — add Horizon, Pulse, or other custom workers
- [Database](../usage/database.md) — `lerd db:import`, `lerd db:shell`, switching engines
- [Services](../usage/services.md) — start Meilisearch, RustFS (S3), Postgres, custom services
- [HTTPS](../features/https.md) — wildcard certs for git worktrees
- [AI Integration (MCP)](../features/mcp.md) — drive lerd from Claude Code, Cursor, etc.
