# Frameworks

Lerd uses **framework definitions** to describe how a PHP project type behaves: where the document root is, how to detect it automatically, which env file to use, and which background workers it supports.

Laravel has a built-in definition. Any other PHP framework (Symfony, WordPress, Drupal, etc.) can be added via a YAML file stored at `~/.config/lerd/frameworks/<name>.yaml`.

---

## Commands

| Command | Description |
|---|---|
| `lerd new <name-or-path>` | Scaffold a new PHP project using a framework's create command |
| `lerd framework list` | List all framework definitions including workers |
| `lerd framework add <name>` | Add or update a framework definition (flags or `--from-file`) |
| `lerd framework remove <name>` | Remove a user-defined framework definition |

---

## Creating new projects

### Laravel installer

Lerd ships with the [Laravel installer](https://laravel.com/docs/installation#creating-a-laravel-application) — it's already available in your CLI after `lerd install`:

```bash
laravel new myapp
cd myapp
lerd link
lerd setup
```

The installer walks you through starter kit selection, database setup, and other options interactively.

### lerd new

`lerd new` is a framework-agnostic shortcut that runs the framework's scaffold command:

```bash
lerd new myapp                          # create ./myapp using Laravel (default)
lerd new myapp --framework=symfony      # create using Symfony's create command
lerd new /path/to/myapp                 # create at an absolute path
lerd new myapp -- --no-interaction      # pass extra flags to the scaffold command
```

For Laravel, this runs:
```bash
composer create-project --no-install --no-plugins --no-scripts laravel/laravel /abs/path/to/myapp
```

After creation, register the site and bootstrap it:
```bash
cd myapp
lerd link
lerd setup
```

Or let your AI assistant do it via MCP:
```
project_new(path: "/home/user/code/myapp")
site_link(path: "/home/user/code/myapp")
env_setup(path: "/home/user/code/myapp")
```

### Defining a create command for custom frameworks

Add a `create` field to a framework's YAML definition. The target directory is appended automatically when `lerd new` runs:

```yaml
# ~/.config/lerd/frameworks/symfony.yaml
name: symfony
create: composer create-project symfony/skeleton
# ... rest of definition
```

Then:
```bash
lerd new myapp --framework=symfony
# runs: composer create-project symfony/skeleton /abs/path/to/myapp
```

---

## Framework workers

Each framework can define one or more **workers** — long-running processes managed as systemd user services inside the PHP-FPM container. Laravel has three built-in: `queue`, `schedule`, and `reverb`.

| Command | Description |
|---|---|
| `lerd worker start <name>` | Start a named worker for the current project |
| `lerd worker stop <name>` | Stop a named worker |
| `lerd worker list` | List all workers defined for this project's framework |

The shortcut commands `lerd queue:start`, `lerd schedule:start`, and `lerd reverb:start` are aliases for `lerd worker start queue/schedule/reverb` — they work for any framework that defines a worker with that name, not just Laravel.

Worker systemd units follow the naming pattern `lerd-<worker>-<sitename>` (e.g. `lerd-messenger-myapp`). Logs:
```bash
journalctl --user -u lerd-messenger-myapp -f
```

---

## Built-in Laravel definition

Laravel is the only framework with a built-in definition. It is always available without any YAML file.

Built-in workers:

| Worker | Label | Command | Restart |
|---|---|---|---|
| `queue` | Queue Worker | `php artisan queue:work --queue=default --tries=3 --timeout=60` | on-failure |
| `schedule` | Task Scheduler | `php artisan schedule:work` | always |
| `reverb` | Reverb WebSocket | `php artisan reverb:start` | on-failure |

The `reverb` worker toggle only appears in the UI when the project actually uses Reverb (detected via `laravel/reverb` in `composer.json` or `BROADCAST_CONNECTION=reverb` in `.env`).

Each site that uses Reverb gets its own `REVERB_SERVER_PORT` assigned automatically — starting at `8080` and incrementing for each additional site — so multiple Reverb-enabled sites can run at the same time without port collisions. The port is written to the site's `.env` on first `lerd env` run or on the first `reverb:start`, and stays fixed after that.

---

## Adding custom workers to Laravel

You can add extra workers to Laravel (e.g. Horizon, Pulse) without overriding its built-in definition. Custom workers are merged on top:

```bash
lerd framework add laravel \
  --from-file horizon.yaml
```

Or inline:

```bash
lerd framework add laravel \
  # (no --public-dir needed for laravel)
```

Using a YAML file (recommended):

```yaml
# horizon.yaml
name: laravel
workers:
  horizon:
    label: Horizon
    command: php artisan horizon
    restart: always
  pulse:
    label: Pulse
    command: php artisan pulse:work
    restart: always
```

```bash
lerd framework add laravel --from-file horizon.yaml
lerd worker start horizon    # starts lerd-horizon-<sitename>
```

To remove your custom additions (the built-in queue/schedule/reverb remain):
```bash
lerd framework remove laravel
```

---

## User-defined frameworks

### Adding a framework

**With flags** (quick):

```bash
lerd framework add symfony \
  --label "Symfony" \
  --public-dir public \
  --detect-file symfony.lock \
  --detect-composer symfony/framework-bundle \
  --env-file .env \
  --env-format dotenv \
  --composer auto \
  --npm auto
```

**From a YAML file** (recommended for sharing):

```bash
lerd framework add symfony --from-file symfony.yaml
```

Framework YAML files are stored at `~/.config/lerd/frameworks/<name>.yaml`.

### Removing a framework

```bash
lerd framework remove symfony
```

---

## YAML schema

```yaml
# Required
name: symfony                     # slug [a-z0-9-], must match filename stem
label: Symfony                    # display name
public_dir: public                # document root relative to project (e.g. public, web, .)

# Detection rules — any match is sufficient
detect:
  - file: symfony.lock            # file must exist in project root
  - composer: symfony/framework-bundle  # package in composer.json require/require-dev

# Env file configuration
env:
  file: .env                      # primary env file (default: .env)
  example_file: .env.dist         # copied to file if missing (like .env.example for Laravel)
  format: dotenv                  # dotenv (default) | php-const (for wp-config.php style)
  fallback_file: wp-config.php    # used when file doesn't exist (optional)
  fallback_format: php-const      # format for fallback_file (optional)
  url_key: DEFAULT_URI            # env key holding the app URL (default: APP_URL)

  # Per-service env detection and variable injection for `lerd env`
  services:
    mysql:
      detect:
        - key: DATABASE_URL
          value_prefix: "mysql://"
      vars:
        - "DATABASE_URL=mysql://root:lerd@lerd-mysql:3306/{{site}}?serverVersion={{mysql_version}}"
    redis:
      detect:
        - key: REDIS_URL
        - key: REDIS_DSN
      vars:
        - "REDIS_URL=redis://lerd-redis:6379"

# Scaffold command for "lerd new" — target directory is appended automatically
create: composer create-project myvendor/myframework

# Dependency installation
composer: auto                    # auto | true | false (auto = run if vendor/ missing)
npm: auto                         # auto | true | false (auto = run if node_modules/ missing)

# Console command (without 'php' prefix)
console: artisan                  # artisan (Laravel), bin/console (Symfony), etc.

# Background workers (systemd user services)
workers:
  messenger:
    label: Messenger               # display name (optional)
    command: php bin/console messenger:consume async --time-limit=3600
    restart: always               # always | on-failure (default: always)
    check:                         # only shown when check passes (optional)
      composer: symfony/messenger

# One-off setup commands shown in `lerd setup` wizard
setup:
  - label: "Run migrations"                     # display name and identifier
    command: "php bin/console doctrine:migrations:migrate --no-interaction"
    default: true                               # pre-selected in the wizard
    check:                                      # only shown when check passes (optional)
      composer: doctrine/doctrine-migrations-bundle
  - label: "Load fixtures"
    command: "php bin/console doctrine:fixtures:load --no-interaction"
    check:
      composer: doctrine/doctrine-fixtures-bundle  # skipped if package not installed
```

---

## Framework detection

When you run `lerd link` or `lerd park`, lerd inspects the project directory and tries to match it against framework definitions in this order:

1. **Laravel** (built-in): checks for `artisan` file or `laravel/framework` in `composer.json`
2. **User-defined frameworks**: iterates `~/.config/lerd/frameworks/*.yaml` alphabetically, applying each detection rule

The **first match wins**. Detection rules are OR-based — any single matching rule is enough to identify the framework.

---

## Document root detection

If no framework matches and no `--public-dir` is specified, lerd tries these candidate directories in order, accepting the first that contains an `index.php`:

`public` → `web` → `webroot` → `pub` → `www` → `htdocs` → `.` (project root)

---

## Web UI

Framework workers appear as toggles in the **Sites** panel alongside queue/schedule/reverb. Each running worker also gets a log tab in the site detail drawer and an indicator dot in the site list.

Workers defined in the framework are shown regardless of framework type — Laravel gets its built-in queue/schedule/reverb workers; Symfony shows the `messenger` worker if defined; etc.

---

## Example: Symfony

```yaml
# ~/.config/lerd/frameworks/symfony.yaml
name: symfony
label: Symfony
detect:
  - file: symfony.lock
  - composer: symfony/framework-bundle
public_dir: public
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
    meilisearch:
      detect:
        - key: MEILISEARCH_HOST
        - key: MEILISEARCH_DSN
      vars:
        - "MEILISEARCH_HOST=http://lerd-meilisearch:7700"
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

```bash
lerd framework add symfony --from-file ~/.config/lerd/frameworks/symfony.yaml
lerd link                          # auto-detected as Symfony
lerd setup                         # shows migrations, fixtures, and other steps
lerd worker start messenger        # starts lerd-messenger-<sitename>
```

---

## Example: WordPress

```yaml
# ~/.config/lerd/frameworks/wordpress.yaml
name: wordpress
label: WordPress
detect:
  - file: wp-login.php
  - file: wp-config.php
public_dir: .
env:
  fallback_file: wp-config.php
  fallback_format: php-const
composer: false
npm: false
```

```bash
lerd framework add wordpress --from-file ~/.config/lerd/frameworks/wordpress.yaml
lerd link                          # auto-detected as WordPress
```
