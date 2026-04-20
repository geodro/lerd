# Framework definitions

Framework definitions are YAML files that tell Lerd how to detect a PHP framework, where its document root is, which env file it uses, and which workers and log paths it has. This page is the full schema reference.

## Definition sources and priority

Lerd resolves framework definitions from multiple sources. Higher priority wins:

| Priority | Source | Location | Purpose |
|----------|--------|----------|---------|
| 1 | User overlay | `~/.config/lerd/frameworks/<name>.yaml` | Manual overrides (merged on top) |
| 2 | Project embedded | `.lerd.yaml` `framework_def` | Portability for user-defined frameworks |
| 3 | Store-installed | `~/.local/share/lerd/frameworks/<name>@<version>.yaml` | Community definitions (auto-fetched) |
| 4 | Built-in | Compiled into lerd binary | Laravel fallback only |

Workers from the user overlay and project `.lerd.yaml` are merged on top of store or built-in definitions. See [Framework workers](framework-workers.md) for the worker lifecycle and how custom workers are added and managed.

## Version resolution

When loading a framework definition for a project, the version is resolved in order:

1. `composer.lock`: the actual installed version (source of truth)
2. `.lerd.yaml` `framework_version`: pinned version (fallback when no `composer.lock`)
3. Latest available in store

When `composer.lock` shows a different version than `.lerd.yaml`, the pinned version is auto-updated.

## Environment setup

The `env` section in a framework definition controls how `lerd env` works:

```yaml
env:
  file: .env                        # primary env file
  example_file: .env.example        # copied to file if missing
  format: dotenv                    # dotenv | php-const
  fallback_file: wp-config.php      # used when file doesn't exist
  fallback_format: php-const        # format for fallback_file
  url_key: APP_URL                  # env key holding the app URL

  # Application key generation
  key_generation:
    env_key: APP_KEY                # env var to check/set
    command: key:generate           # artisan command to run if vendor/ exists
    fallback_prefix: "base64:"     # prefix for random key fallback

  # Per-service detection and env variable injection
  services:
    mysql:
      detect:
        - key: DB_CONNECTION
          value_prefix: mysql
      vars:
        - DB_CONNECTION=mysql
        - DB_HOST=lerd-mysql
        - DB_PORT=3306
        - DB_DATABASE={{site}}
        - DB_USERNAME=root
        - DB_PASSWORD=lerd
```

## YAML schema

```yaml
# Required
name: symfony                     # slug [a-z0-9-], must match filename stem
label: Symfony                    # display name
public_dir: public                # document root relative to project

# Version (required for store definitions)
version: "8"                      # framework major version this definition targets

# PHP version range (optional, used during lerd link/init to clamp PHP version)
php:
  min: "8.2"                      # minimum supported PHP version
  max: "8.5"                      # maximum supported PHP version

# Detection rules, any match is sufficient
detect:
  - file: symfony.lock
  - composer: symfony/framework-bundle

# Env file configuration
env:
  file: .env.local
  example_file: .env
  format: dotenv                  # dotenv | php-const
  fallback_file: settings.php     # used when file doesn't exist (optional)
  fallback_format: php-const
  url_key: DEFAULT_URI            # env key holding the app URL (default: APP_URL)
  key_generation:                 # application key generation (optional)
    env_key: APP_KEY
    command: key:generate
    fallback_prefix: "base64:"

  # Per-service env detection and variable injection for `lerd env`
  #
  # Template variables available in vars values:
  #   {{site}}              : project database / handle name (e.g. myapp)
  #   {{site_testing}}      : testing database name (e.g. myapp_testing)
  #   {{bucket}}            : S3-safe bucket name (lowercase, hyphens; e.g. my-app)
  #   {{domain}}            : site's primary domain (e.g. myapp.test)
  #   {{scheme}}            : http or https depending on TLS status
  #   {{mysql_version}}     : running MySQL server version
  #   {{postgres_version}}  : running PostgreSQL server version
  #   {{redis_version}}     : running Redis server version
  #   {{meilisearch_version}} : running Meilisearch server version
  services:
    mysql:
      detect:
        - key: DATABASE_URL
          value_prefix: "mysql://"
      vars:
        - "DATABASE_URL=mysql://root:lerd@lerd-mysql:3306/{{site}}"

# Scaffold command for "lerd new"
create: composer create-project symfony/skeleton

# Dependency installation
composer: auto                    # auto | true | false
npm: auto

# Console command (without 'php' prefix)
console: bin/console

# Background workers
workers:
  messenger:
    label: Messenger
    command: php bin/console messenger:consume async --time-limit=3600
    restart: always               # always | on-failure (default: always)
    schedule: ""                  # systemd OnCalendar expression (optional). When set, the
                                  # worker is run as a Type=oneshot service triggered by a
                                  # sibling .timer instead of a long-running daemon. Use this
                                  # for cron-style commands like Laravel <=10's
                                  # `php artisan schedule:run`, which exits immediately and
                                  # would otherwise restart-loop under restart=always. Any
                                  # systemd OnCalendar value is accepted (e.g. `minutely`,
                                  # `*:0/5`, `Mon..Fri *-*-* 02:00:00`). Linux only; on
                                  # macOS scheduled workers currently log a warning and skip.
    check:                        # only shown when check passes (optional)
      composer: symfony/messenger
    conflicts_with:               # workers to stop before starting (optional)
      - other-worker
    proxy:                        # nginx proxy config (optional)
      path: /ws
      port_env_key: WS_PORT
      default_port: 8080

# One-off setup commands
setup:
  - label: "Run migrations"
    command: "php bin/console doctrine:migrations:migrate --no-interaction"
    default: true
    check:
      composer: doctrine/doctrine-migrations-bundle  # skipped if package not installed

# Application log files shown in the UI "App Logs" tab
logs:
  - path: "var/log/*.log"             # glob relative to project root
    format: raw                       # monolog | raw (plain text, default)
```

## Framework detection

Framework detection only runs during `lerd link`, `lerd init`, `lerd env`, `lerd setup`, and `lerd park`. All other commands read the saved framework from the site registry.

Detection order:

1. **Laravel** (built-in): checks for `artisan` file or `laravel/framework` in `composer.json`
2. **Local definitions**: iterates user-defined and store-installed YAML files, applying detection rules
3. **Framework store** (interactive): checks the store index and prompts to install, or fetches silently when `.lerd.yaml` specifies the framework name

The first match wins. Detection rules are OR-based, any single matching rule is enough.

## Document root detection

If no framework matches and no `--public-dir` is specified, lerd tries these candidate directories in order, accepting the first that contains an `index.php`:

`public` → `web` → `webroot` → `pub` → `www` → `htdocs` → `.` (project root)

## Log viewer

Frameworks can define application log file locations so they appear in the UI's **App Logs** tab. The tab only appears when matching log files actually exist on disk; for example, WordPress defines `wp-content/debug.log` but the tab stays hidden until `WP_DEBUG_LOG` is enabled. Custom frameworks can add their own:

```yaml
logs:
  - path: "var/log/*.log"
    format: raw
```

The `path` is a glob relative to the project root. The `format` controls parsing:

| Format | Description |
|---|---|
| `monolog` | Monolog format: `[date] channel.LEVEL: message {context}` with stacktrace grouping |
| `raw` | Plain text, each line shown as a separate entry (default) |

The App Logs tab is the first tab in the site detail view. When the UI opens it automatically selects the site with the most recent log activity, so you immediately see logs from the project you last visited in your browser.

Features:

- **File selector**: switch between available log files (e.g. `laravel.log`, `worker.log`), sorted by modification time with the newest file pre-selected
- **Latest / All toggle**: "Latest" shows the last 100 entries (default), "All" reads the entire file
- **Search**: filter entries by message, level, date, or stacktrace content
- **Expandable entries**: click any entry to expand and see the full detail and stacktrace
- **Auto-refresh**: polls every 5 seconds while the tab is active, keeping the expanded entry open
- **Color-coded levels**: entries are color-coded by severity (red for ERROR/CRITICAL/EMERGENCY/ALERT, yellow for WARNING, blue for INFO/NOTICE, grey for DEBUG)

To customise Laravel's log paths (e.g. add a custom channel log):

```yaml
# ~/.config/lerd/frameworks/laravel.yaml
name: laravel
logs:
  - path: "storage/logs/*.log"
    format: monolog
  - path: "storage/logs/custom/*.log"
    format: monolog
```

---

See also: [Frameworks](frameworks.md) for the store and commands; [Framework workers](framework-workers.md) for worker lifecycle.
