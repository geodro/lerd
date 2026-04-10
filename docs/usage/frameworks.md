# Frameworks

Lerd uses **framework definitions** to describe how a PHP project type behaves: where the document root is, how to detect it automatically, which env file to use, and which background workers it supports.

Laravel has a built-in definition. Other frameworks (Symfony, WordPress, Drupal, CakePHP, Statamic, etc.) can be installed from the [community store](https://github.com/geodro/lerd-frameworks) or defined manually.

---

## Commands

| Command | Description |
|---|---|
| `lerd new <name-or-path>` | Scaffold a new PHP project using a framework's create command |
| `lerd framework list` | List all framework definitions with source and workers |
| `lerd framework list --check` | Compare local definitions against the store |
| `lerd framework search [query]` | Search the community store for available definitions |
| `lerd framework install <name>[@version]` | Install a framework definition from the store |
| `lerd framework update [name[@version]]` | Update installed definitions from the store |
| `lerd framework update --diff` | Preview changes before applying updates |
| `lerd framework add <name>` | Add or update a user-defined framework definition |
| `lerd framework remove <name>[@version]` | Remove a framework definition (prompts if multiple versions) |
| `lerd framework remove <name> --all` | Remove all versions of a framework definition |

---

## Framework store

Lerd has a community-driven framework store backed by [geodro/lerd-frameworks](https://github.com/geodro/lerd-frameworks). The store hosts definitions for popular PHP frameworks, versioned by major release.

### Available frameworks

```bash
lerd framework search
```

```
Name            Label           Latest       Versions
───────────────────────────────────────────────────────
laravel         Laravel         13           13, 12, 11, 10
symfony         Symfony         8            8, 7
wordpress       WordPress       6            6, 5
drupal          Drupal          11           11, 10
cakephp         CakePHP         5            5, 4
statamic        Statamic        6            6, 5
```

### Installing from the store

```bash
lerd framework install symfony          # auto-detects version from composer.lock
lerd framework install laravel@12       # explicit version
lerd framework install wordpress        # latest version
```

When no version is specified, lerd reads `composer.lock` to detect the installed major version. If the version can't be determined, it falls back to the latest available.

Store-installed definitions are saved to `~/.local/share/lerd/frameworks/<name>@<version>.yaml`, separate from user-defined frameworks.

### Checking for updates

```bash
lerd framework list --check
```

```
Name            Version  Source     Latest     Status
───────────────────────────────────────────────────────
laravel         —        built-in   13         built-in
symfony         8        store      8          up to date
wordpress       6        store      6          up to date
magento         —        user       —          not in store
```

### Updating

```bash
lerd framework update symfony         # update a single framework
lerd framework update symfony@7       # update to a specific version
lerd framework update                 # update all installed frameworks
lerd framework update --diff          # show changes before applying
```

### Auto-detection and auto-fetch

When any command needs a framework definition that isn't installed locally, lerd fetches it from the store automatically. The version is resolved from `composer.lock`, so a Laravel 11 project gets `laravel@11.yaml` and a Laravel 12 project gets `laravel@12.yaml`.

Locally installed definitions are refreshed from the store every 24 hours to pick up upstream fixes (e.g. new log sources, corrected PHP ranges).

During `lerd link`, `lerd init`, or `lerd setup`, if no framework is detected at all:

- **Interactive mode**: prompts to install from the store
- **Non-interactive mode**: fetches silently when `.lerd.yaml` specifies a framework name

### Contributing to the store

Submit a pull request to [geodro/lerd-frameworks](https://github.com/geodro/lerd-frameworks) with a YAML file under `frameworks/<name>/<version>.yaml` and update `frameworks/index.json`.

---

## Definition sources and priority

Lerd resolves framework definitions from multiple sources. Higher priority wins:

| Priority | Source | Location | Purpose |
|----------|--------|----------|---------|
| 1 | User overlay | `~/.config/lerd/frameworks/<name>.yaml` | Manual overrides (merged on top) |
| 2 | Project embedded | `.lerd.yaml` `framework_def` | Portability for user-defined frameworks |
| 3 | Store-installed | `~/.local/share/lerd/frameworks/<name>@<version>.yaml` | Community definitions (auto-fetched) |
| 4 | Built-in | Compiled into lerd binary | Laravel fallback only |

### Worker merging

When a store or built-in definition is used, workers from the user-defined overlay (`~/.config/lerd/frameworks/<name>.yaml`) are merged on top. Project-specific custom workers from `.lerd.yaml` are also merged. This means you can add workers without replacing the base definition:

```yaml
# ~/.config/lerd/frameworks/laravel.yaml — adds Pulse to the built-in Laravel definition
name: laravel
workers:
  pulse:
    label: Pulse
    command: php artisan pulse:work
    restart: always
```

### Managing custom workers

Use `lerd worker add` to add project-specific or global custom workers without manually editing YAML:

```bash
# Add a project-specific worker (saved to .lerd.yaml)
lerd worker add pulse --command "php artisan pulse:work" --label "Pulse" --check-composer laravel/pulse

# Add a worker that conflicts with another (stops it on start, hides it in UI)
lerd worker add custom-queue --command "php artisan queue:work --queue=emails" --conflicts-with queue

# Add a global worker (saved to ~/.config/lerd/frameworks/<name>.yaml)
lerd worker add pulse --command "php artisan pulse:work" --global

# Remove a custom worker (stops it if running)
lerd worker remove pulse
lerd worker remove pulse --global
```

Project workers (`.lerd.yaml`) apply to a single project and are committed to git. Global workers (user overlay) apply to all projects using that framework. Both survive framework store updates.

The resulting `.lerd.yaml` looks like:

```yaml
framework: laravel
custom_workers:
  pulse:
    label: Pulse
    command: php artisan pulse:work
    check:
      composer: laravel/pulse
  custom-queue:
    command: php artisan queue:work --queue=emails
    conflicts_with:
      - queue
```

After adding, start the worker with `lerd worker start pulse`.

When running `lerd init --fresh`, existing custom workers are shown in a multi-select step before the workers step. Deselecting a custom worker removes it from `.lerd.yaml` and excludes it from the workers selection. If the removed worker had `conflicts_with`, those workers become available again.

### Orphaned workers

A worker becomes orphaned when its systemd unit is still running but its definition has been removed from `.lerd.yaml` (e.g. after a `git pull` or manual edit). Orphaned workers are detected and surfaced in several places:

- **`lerd worker list`** — shows orphaned workers with a stop hint
- **`lerd worker stop <name>`** — can stop orphaned workers even without a definition
- **`lerd setup`** — offers orphaned workers as pre-selected stop steps before framework worker starts
- **UI** — the stop button works for orphaned workers directly

### Version resolution

When loading a framework definition for a project, the version is resolved in order:

1. `composer.lock` — the actual installed version (source of truth)
2. `.lerd.yaml` `framework_version` — pinned version (fallback when no `composer.lock`)
3. Latest available in store

When `composer.lock` shows a different version than `.lerd.yaml`, the pinned version is auto-updated.

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
lerd new myapp                          # create using Laravel (default)
lerd new myapp --framework=symfony      # create using Symfony's create command
lerd new /path/to/myapp                 # create at an absolute path
lerd new myapp -- --no-interaction      # pass extra flags to the scaffold command
```

After creation:
```bash
cd myapp
lerd link
lerd setup
```

---

## Framework workers

Each framework can define **workers** — long-running processes managed as systemd user services inside the PHP-FPM container.

| Command | Description |
|---|---|
| `lerd worker start <name>` | Start a named worker for the current project |
| `lerd worker stop <name>` | Stop a named worker |
| `lerd worker list` | List all workers defined for this project's framework |

The shortcut commands `lerd queue:start`, `lerd schedule:start`, `lerd reverb:start`, and `lerd horizon:start` are aliases — they look up the worker from the framework definition and delegate to the generic handler. They work for any framework that defines a worker with that name.

### Worker features

**Conditional workers** — Workers with a `check` rule only appear when the condition passes (e.g. `laravel/horizon` is in `composer.json`):

```yaml
workers:
  horizon:
    command: php artisan horizon
    check:
      composer: laravel/horizon
```

**Conflict resolution** — Workers can declare conflicts. When a conflicting worker starts, the other is stopped automatically and hidden from the UI:

```yaml
workers:
  horizon:
    command: php artisan horizon
    conflicts_with:
      - queue      # stops queue before starting horizon; hides queue toggle in UI
```

**WebSocket/HTTP proxy** — Workers that need an nginx proxy block define a `proxy` config. Lerd auto-assigns a collision-free port and regenerates the nginx vhost:

```yaml
workers:
  reverb:
    command: php artisan reverb:start
    proxy:
      path: /app                    # URL path for the proxy location block
      port_env_key: REVERB_SERVER_PORT  # env key holding the port
      default_port: 8080            # starting port for auto-assignment
```

Port assignment scans all proxy port env keys across all sites to prevent collisions between different workers and frameworks.

### Project-specific custom workers

Add workers to `.lerd.yaml` for project-specific needs that don't belong in the framework definition:

```yaml
# .lerd.yaml
framework: symfony
framework_version: "8"
workers:
  - messenger
  - pdf-generator
custom_workers:
  pdf-generator:
    label: PDF Generator
    command: php bin/console app:generate-pdfs --daemon
    restart: always
```

Custom workers with proxy support:

```yaml
custom_workers:
  mercure:
    label: Mercure Hub
    command: php bin/console mercure:run
    restart: always
    proxy:
      path: /.well-known/mercure
      port_env_key: MERCURE_PORT
      default_port: 3000
```

Custom workers are merged with the framework's workers at runtime. They are committed to git so teammates get the same setup.

### Worker logs

```bash
journalctl --user -u lerd-messenger-myapp -f
```

---

## Laravel definition

Laravel has a built-in definition compiled into the binary as a fallback. When a project is linked, lerd auto-fetches the version-specific definition from the store (e.g. `laravel@11`, `laravel@12`), which includes the correct PHP version range and version-specific behaviour (e.g. Laravel 10 uses `schedule:run` instead of `schedule:work`, and doesn't include Reverb).

Default workers:

| Worker | Label | Command | Check | Extra |
|---|---|---|---|---|
| `queue` | Queue Worker | `php artisan queue:work --queue=default --tries=3 --timeout=60` | — | — |
| `schedule` | Task Scheduler | `php artisan schedule:work` | — | — |
| `reverb` | Reverb WebSocket | `php artisan reverb:start` | `laravel/reverb` | proxy at `/app`, auto-assigned port |
| `horizon` | Horizon | `php artisan horizon` | `laravel/horizon` | conflicts with `queue` |

### Adding workers to Laravel

User-defined workers are merged on top of the built-in. Use `lerd framework add` to create an overlay:

```yaml
# horizon.yaml
name: laravel
workers:
  pulse:
    label: Pulse
    command: php artisan pulse:work
    restart: always
```

```bash
lerd framework add laravel --from-file horizon.yaml
```

To remove the overlay (built-in workers remain):
```bash
lerd framework remove laravel
```

### Removing framework definitions

```bash
lerd framework remove symfony          # prompts if multiple versions installed
lerd framework remove symfony@7        # remove a specific version
lerd framework remove symfony --all    # remove all versions
```

When multiple versions of a framework are installed, `lerd framework remove` prompts you to choose which version to remove.

---

## PHP version clamping

When a framework definition includes `php.min` and `php.max`, `lerd link` and `lerd init` automatically clamp the detected PHP version to the supported range. For example, if you link a Laravel 10 project (max PHP 8.3) but your system defaults to PHP 8.4, lerd will select PHP 8.3 instead:

```
PHP 8.4 is outside Laravel's supported range (8.1–8.3), using PHP 8.3.
```

This prevents accidentally running a project on an unsupported PHP version.

---

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

---

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

# Detection rules — any match is sufficient
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
  #   {{site}}              — project database / handle name (e.g. myapp)
  #   {{site_testing}}      — testing database name (e.g. myapp_testing)
  #   {{domain}}            — site's primary domain (e.g. myapp.test)
  #   {{scheme}}            — http or https depending on TLS status
  #   {{mysql_version}}     — running MySQL server version
  #   {{postgres_version}}  — running PostgreSQL server version
  #   {{redis_version}}     — running Redis server version
  #   {{meilisearch_version}} — running Meilisearch server version
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

---

## Framework detection

Framework detection only runs during `lerd link`, `lerd init`, `lerd env`, `lerd setup`, and `lerd park`. All other commands read the saved framework from the site registry.

Detection order:

1. **Laravel** (built-in): checks for `artisan` file or `laravel/framework` in `composer.json`
2. **Local definitions**: iterates user-defined and store-installed YAML files, applying detection rules
3. **Framework store** (interactive): checks the store index and prompts to install, or fetches silently when `.lerd.yaml` specifies the framework name

The first match wins. Detection rules are OR-based — any single matching rule is enough.

---

## Document root detection

If no framework matches and no `--public-dir` is specified, lerd tries these candidate directories in order, accepting the first that contains an `index.php`:

`public` → `web` → `webroot` → `pub` → `www` → `htdocs` → `.` (project root)

---

## Log viewer

Frameworks can define application log file locations so they appear in the UI's **App Logs** tab. The tab only appears when matching log files actually exist on disk — for example, WordPress defines `wp-content/debug.log` but the tab stays hidden until `WP_DEBUG_LOG` is enabled. Custom frameworks can add their own:

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

- **File selector** — switch between available log files (e.g. `laravel.log`, `worker.log`), sorted by modification time with the newest file pre-selected
- **Latest / All toggle** — "Latest" shows the last 100 entries (default), "All" reads the entire file
- **Search** — filter entries by message, level, date, or stacktrace content
- **Expandable entries** — click any entry to expand and see the full detail and stacktrace
- **Auto-refresh** — polls every 5 seconds while the tab is active, keeping the expanded entry open
- **Color-coded levels** — entries are color-coded by severity (red for ERROR/CRITICAL/EMERGENCY/ALERT, yellow for WARNING, blue for INFO/NOTICE, grey for DEBUG)

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

## Web UI

Framework workers appear as toggles in the Sites panel. Workers with a `check` rule only appear when the condition passes. Workers with `conflicts_with` suppress each other (e.g. when Horizon is available, the queue toggle is hidden).

Custom framework workers from `.lerd.yaml` also appear as toggles alongside the framework's standard workers.
