# Symfony walkthrough

End-to-end: from `lerd install` to a Symfony app running on `https://myapp.test` with Doctrine, MySQL, and a Messenger worker.

::: info Prerequisites
You've already run `lerd install` once on this machine. If not, see [Installation](installation.md).
:::

::: tip Drive it from your AI assistant
Run `lerd mcp:enable-global` once and your AI assistant (Claude Code, Junie, Windsurf) can call every command below as an MCP tool — `project_new`, `site_link`, `env_setup`, `db_create`, `secure`, `worker_start`, etc. See [AI Integration](../features/mcp.md).
:::

---

## 1. Register the Symfony framework definition (one-time)

Lerd's built-in framework is Laravel. Other frameworks are user-defined YAML files dropped into `~/.config/lerd/frameworks/`. Save this as `~/.config/lerd/frameworks/symfony.yaml`:

```yaml
# ~/.config/lerd/frameworks/symfony.yaml
name: symfony
label: Symfony
detect:
  - file: symfony.lock
  - composer: symfony/framework-bundle
public_dir: public
create: composer create-project symfony/skeleton
console: bin/console
env:
  file: .env
  example_file: .env.dist
  format: dotenv
  url_key: DEFAULT_URI
  services:
    mysql:
      detect:
        - key: DATABASE_URL
          value_prefix: "mysql://"
        - key: DATABASE_URL
          value_prefix: "mariadb://"
      vars:
        - "DATABASE_URL=mysql://root:lerd@lerd-mysql:3306/{{site}}?serverVersion={{mysql_version}}"
    postgres:
      detect:
        - key: DATABASE_URL
          value_prefix: "postgresql://"
        - key: DATABASE_URL
          value_prefix: "postgres://"
      vars:
        - "DATABASE_URL=postgresql://postgres:lerd@lerd-postgres:5432/{{site}}?serverVersion={{postgres_version}}"
    redis:
      detect:
        - key: REDIS_URL
        - key: REDIS_DSN
      vars:
        - "REDIS_URL=redis://lerd-redis:6379"
    mailpit:
      detect:
        - key: MAILER_DSN
      vars:
        - "MAILER_DSN=smtp://lerd-mailpit:1025"
composer: auto
npm: auto
workers:
  messenger:
    label: Messenger
    command: php bin/console messenger:consume async --time-limit=3600
    restart: always
    check:
      composer: symfony/messenger
setup:
  - label: "Run migrations"
    command: "php bin/console doctrine:migrations:migrate --no-interaction --allow-no-migration"
    default: true
    check:
      composer: doctrine/doctrine-migrations-bundle
  - label: "Load fixtures"
    command: "php bin/console doctrine:fixtures:load --no-interaction"
    check:
      composer: doctrine/doctrine-fixtures-bundle
  - label: "Clear cache"
    command: "php bin/console cache:clear"
    default: true
```

Then register it with lerd:

```bash
lerd framework add symfony --from-file ~/.config/lerd/frameworks/symfony.yaml
```

You only do this once per machine. From now on, every Symfony project is auto-detected via `symfony.lock` or `symfony/framework-bundle`.

See [Frameworks & Workers](../usage/frameworks.md) for the full schema reference.

---

## 2. Create the project

::: code-group

```bash [lerd new]
cd ~/Lerd
lerd new myapp --framework=symfony
# runs: composer create-project symfony/skeleton ./myapp
```

```bash [composer]
cd ~/Lerd
composer create-project symfony/skeleton myapp
```

```bash [existing repo]
cd ~/Lerd
git clone git@github.com:you/myapp.git
```

:::

---

## 3. Register the site

```bash
cd myapp
lerd link
```

`lerd link` detects Symfony (via `symfony.lock` or the composer package), assigns `http://myapp.test`, and sets the document root to `public/`.

---

## 4. Configure PHP, Node, database, services

```bash
lerd init
```

```
? PHP version: 8.4
? Node version (leave blank to skip): 22
? Enable HTTPS? Yes
? Database: mysql
? Services: [mailpit]
? Workers to auto-start: [messenger]
Saved .lerd.yaml
```

The wizard discovers `messenger` as an available worker because the framework YAML declares it (and the `check: composer: symfony/messenger` rule matches your project).

---

## 5. Bootstrap the project

```bash
lerd setup
```

```
? Select setup steps to run:
  ◉ composer install
  ◉ npm ci                       # only if package.json exists
  ◉ lerd env                     # injects DATABASE_URL, MAILER_DSN, DEFAULT_URI
  ◉ Run migrations               # from framework setup block
  ◉ Clear cache                  # from framework setup block
  ◯ Load fixtures
  ◉ lerd secure                  # mkcert TLS for myapp.test
  ◉ messenger:start
  ◉ lerd open
```

The "Run migrations", "Clear cache", and "Load fixtures" steps come from the `setup:` block in your `symfony.yaml`. Lerd surfaces them automatically and respects the `check:` rules — fixtures only appears if `doctrine/doctrine-fixtures-bundle` is installed.

When it finishes, `https://myapp.test` opens in your browser and `lerd-messenger-myapp` is running as a systemd user service.

---

## 6. Verify

```bash
lerd status
```

```bash
# Tail messenger logs
journalctl --user -u lerd-messenger-myapp -f
```

App logs (anything in `var/log/*.log`) show up in the [Web UI](../features/web-ui.md) **App Logs** tab — add a `logs:` block to `symfony.yaml` to customise paths or parsing.

---

## What just happened

| Command | What it did |
|---|---|
| `lerd framework add symfony` | Registered the YAML so Symfony projects are auto-detected |
| `lerd link` | Assigned `myapp.test`, set document root to `public/` |
| `lerd init` | Wrote `.lerd.yaml` with PHP, Node, MySQL, Mailpit, messenger |
| `lerd env` (via setup) | Wrote `DATABASE_URL=mysql://root:lerd@lerd-mysql:3306/myapp?serverVersion=8.0` and `MAILER_DSN=smtp://lerd-mailpit:1025` into `.env` |
| `lerd secure` (via setup) | Issued mkcert cert, set `DEFAULT_URI=https://myapp.test` |
| Doctrine migrations + cache:clear | Ran via the framework's `setup:` block |
| `lerd worker start messenger` (via setup) | Launched `lerd-messenger-myapp` |

---

## Next steps

- [Frameworks & Workers](../usage/frameworks.md) — add custom workers, customise log paths, define more setup steps
- [Database](../usage/database.md) — `lerd db:import`, `lerd db:shell`, switching to Postgres
- [Services](../usage/services.md) — start Meilisearch, RustFS (S3), custom services
- [HTTPS](../features/https.md) — how `lerd secure` works under the hood
- [AI Integration (MCP)](../features/mcp.md) — drive lerd from Claude Code, Cursor, Junie, etc.
