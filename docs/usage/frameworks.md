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
laravel         -        built-in   13         built-in
symfony         8        store      8          up to date
wordpress       6        store      6          up to date
magento         -        user       -          not in store
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

## Creating new projects

### Laravel installer

Lerd ships with the [Laravel installer](https://laravel.com/docs/installation#creating-a-laravel-application); it's already available in your CLI after `lerd install`:

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

## Laravel definition

Laravel has a built-in definition compiled into the binary as a fallback. When a project is linked, lerd auto-fetches the version-specific definition from the store (e.g. `laravel@11`, `laravel@12`), which includes the correct PHP version range and version-specific behaviour (e.g. Laravel 10 uses `schedule:run` instead of `schedule:work`, and doesn't include Reverb).

Default workers:

| Worker | Label | Command | Check | Extra |
|---|---|---|---|---|
| `queue` | Queue Worker | `php artisan queue:work --queue=default --tries=3 --timeout=60` | - | - |
| `schedule` | Task Scheduler | `php artisan schedule:work` | - | - |
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

When a framework definition includes `php.min` and `php.max`, `lerd link` and `lerd init` automatically clamp the detected PHP version to the supported range. For example, if you link a Laravel 10 project (max PHP 8.3) but your system defaults to PHP 8.5, lerd will select PHP 8.3 instead:

```
PHP 8.5 is outside Laravel's supported range (8.1-8.3), using PHP 8.3.
```

This prevents accidentally running a project on an unsupported PHP version.

---

## More

- [Framework workers](framework-workers.md): conditional rules, conflicts, proxy wiring, project custom workers, orphaned workers.
- [Framework definitions](framework-definitions.md): YAML schema, env setup, detection rules, doc-root fallback, log viewer.
