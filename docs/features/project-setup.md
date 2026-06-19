# Project Setup

Two commands cover project bootstrap:

| Command | What it does |
|---|---|
| `lerd init` | Runs the interactive wizard, writes `.lerd.yaml`, applies it (link, HTTPS, database, services) |
| `lerd setup` | Runs `lerd init` first, then a checkbox list of install/migrate/build steps |

Use `lerd init` when you only want to configure the site (PHP version, services, HTTPS). Use `lerd setup` when you also want to install dependencies, run migrations, and start workers in one go.

---

## `lerd init`

`lerd init` is the entry point for `.lerd.yaml`. Run it from the project root:

```bash
cd ~/Projects/my-app
lerd init
```

```
→ Configuring site...
? PHP version: 8.5
? Node version (leave blank to skip): 22
? Enable HTTPS? No
? Database: mysql
? Services: [mysql, redis]
? Workers to auto-start: [queue, schedule]
Saved .lerd.yaml
Linked: my-app -> my-app.test (PHP 8.5, Node 22, Framework: laravel)
```

The answers are saved to `.lerd.yaml` in the project root and applied immediately: the site is linked, HTTPS is enabled if requested, the database is created, and the chosen services are started.

The services list includes both built-in services and any custom services already registered with `lerd service add`. The workers step pre-selects workers based on the framework and detected packages; Horizon is shown automatically when `laravel/horizon` is in `composer.json`, replacing the generic queue option.

**Commit `.lerd.yaml` to your repo.** On any future machine, running `lerd link` (or `lerd init` again) reads the saved file and restores the full configuration non-interactively, no prompts.

| Flag | Description |
|---|---|
| `--fresh` | Re-run the wizard with existing `.lerd.yaml` values as defaults instead of applying them silently |

Use `--fresh` when you want to change the database engine, swap PHP versions, add or remove services, or otherwise re-answer the wizard.

---

## Migrating from Herd, DDEV or Lando

When you run `lerd init` in a project that still carries a `herd.yml`, `.ddev/config.yaml`, or `.lando.yml`, lerd detects it and offers to pre-fill the wizard from that file:

```
Detected herd.yml. Use it for wizard defaults? [Y/n]
```

Accept and the PHP version, Node version, HTTPS preference, document root, domains, and database/services are translated into the wizard's defaults, which you then review and confirm exactly like any other run. Decline and the wizard starts from plain auto-detection instead.

The translation is intentionally partial, and anything it drops or changes is printed before the wizard opens:

| Source field | Becomes | Note |
|---|---|---|
| PHP version | `php_version` | direct |
| `secured` (Herd) | HTTPS | direct |
| docroot / webroot | `public_dir` | direct |
| project name + aliases / hostnames / proxy | `domains` | wildcards and full FQDNs are skipped |
| database engine | a database service | pinned versions and ports are dropped; lerd resolves them per machine |

MariaDB folds into MySQL because lerd's `mariadb` preset is an opt-in alternate; run `lerd service preset mariadb` first if you need MariaDB specifically. The framework is never translated from the source file because lerd auto-detects it. The seed only ever runs when no `.lerd.yaml` exists yet, so it never overwrites an existing lerd configuration.

---

## `lerd setup`

`lerd setup` is the one-shot bootstrap command for a fresh PHP project. It runs `lerd init` first (so the wizard described above appears), then shows a checkbox list of install/migrate/build steps:

```bash
cd ~/Projects/my-app
lerd setup
```

After the wizard, a checkbox list appears with all available steps pre-selected based on the current project state. Worker steps are pre-selected based on the `.lerd.yaml` workers list:

```
? Select setup steps to run:
  ◉ composer install
  ◉ npm ci
  ◉ lerd env
  ◯ lerd mcp:inject
  ◉ php artisan migrate
  ◯ php artisan db:seed
  ◉ php artisan storage:link
  ◉ npm run build
  ◯ lerd secure
  ◉ queue:start
  ◉ lerd open
```

The `lerd secure` step is omitted entirely when HTTPS was already enabled in the init wizard, because there is nothing left to do.

On a machine where `.lerd.yaml` already exists the wizard is skipped and the saved configuration is applied silently before the step selector appears.

`lerd link` also applies `.lerd.yaml` when the file is present, so cloning a repo and running `lerd link` is enough to restore the full environment without running `lerd setup` or `lerd init` first. When workers are configured in `.lerd.yaml` but not yet running, `lerd link` prompts to run `lerd setup` so you can install dependencies, run migrations, and start workers in the right order.

## Running a command in an unlinked project

Commands that operate on the current site (`lerd open`, `lerd runtime`, `lerd worker`, `lerd stripe`, `lerd env`, `lerd domain`, `lerd share`, `lerd xdebug`) need the directory to be linked. When you run one in a project that has not been linked yet, lerd offers to link it for you instead of stopping with an error:

```
This directory isn't linked to lerd. Link it now? [Y/n]
```

Accepting runs `lerd link` (which cascades into `lerd init` when there is no `.lerd.yaml`), then the original command continues, so a single command is enough rather than a link, init, retry sequence. In a non-interactive context (a script or CI) the command returns a single clear error and does nothing else.

See [Configuration](../configuration.md#per-project-config-lerdyaml) for the full field reference including inline service definitions and custom frameworks.

---

## Automatic version switching

When the Lerd watcher is running it monitors `.lerd.yaml`, `.php-version`, `.node-version`, and `.nvmrc` in every linked site directory. If any of these files change, for example after a `git checkout` to a branch with a different `.lerd.yaml`, Lerd automatically:

1. Re-detects the PHP and Node versions for the site.
2. Updates the site registry.
3. Regenerates the nginx vhost (when the PHP version changed) and reloads nginx.

No hooks or per-project setup needed; it works for every linked site out of the box.

---

## Smart defaults

| Step | Default | Condition |
|---|---|---|
| `composer install` | - [x] on | only if `vendor/` is missing; runs inside the project's PHP-FPM container to match the `composer.json` PHP constraint |
| `npm ci` | - [x] on | only if `node_modules/` is missing and `package.json` exists |
| `lerd env` | - [x] on | always |
| `lerd mcp:inject` | - [ ] off | opt-in |
| `php artisan migrate` | - [x] on | always |
| `php artisan db:seed` | - [ ] off | opt-in |
| `php artisan storage:link` | - [x] on | only if `storage/app/public` is not yet symlinked |
| `npm run build` | - [x] on | only if `package.json` exists |
| `lerd secure` | - [ ] off | opt-in |
| `queue:start` | - [x] on | only if `QUEUE_CONNECTION=redis` is set in `.env` or `.env.example` |
| `lerd open` | - [x] on | always |

The asset build step detects the right command from `package.json`; it looks for `build`, `production`, or `prod` scripts in priority order.

---

## Error handling

If a step fails, you are prompted to continue or abort:

```
✗ migrate failed: exit status 1
  Continue with remaining steps? [y/N]:
```

---

## Flags

| Flag | Description |
|---|---|
| `--all` / `-a` | Select all steps without showing the prompt (CI/automation) |
| `--skip-open` | Skip opening the browser at the end |
