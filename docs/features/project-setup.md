# Project Setup

`lerd setup` automates the standard steps for getting a fresh PHP project running locally. Run it from the project root:

```bash
cd ~/Projects/my-app
lerd setup
```

Before the step selector, `lerd setup` runs the **lerd init wizard** — you choose the PHP version, Node version, HTTPS, and required services. The answers are saved to `.lerd.yaml` in the project root. Commit this file so on any future machine `lerd setup` (or even `lerd link`) reads it and skips the wizard entirely.

```
→ Configuring site...
? PHP version: 8.4
? Node version (leave blank to skip): 22
? Enable HTTPS? No
? Services: [mysql, redis]
Saved .lerd.yaml
Linked: my-app -> my-app.test (PHP 8.4, Node 22, Framework: laravel)
```

The services list includes both built-in services and any custom services already registered with `lerd service add`.

The init wizard also includes a workers step that lets you select which workers to auto-start. Available workers depend on the framework — Horizon is shown automatically when `laravel/horizon` is detected in `composer.json`, replacing the generic queue option.

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

The `lerd secure` step is omitted entirely when HTTPS was already enabled in the init wizard — there is nothing left to do.

On a machine where `.lerd.yaml` already exists the wizard is skipped and the saved configuration is applied silently before the step selector appears.

`lerd link` also applies `.lerd.yaml` when the file is present, so cloning a repo and running `lerd link` is enough to restore the full environment without running `lerd setup` or `lerd init` first. When workers are configured in `.lerd.yaml` but not yet running, `lerd link` prompts to run `lerd setup` so you can install dependencies, run migrations, and start workers in the right order.

See [Configuration](../reference/configuration.md#per-project-config-lerdyaml) for the full field reference including inline service definitions and custom frameworks.

---

## Automatic version switching

When the Lerd watcher is running it monitors `.lerd.yaml`, `.php-version`, `.node-version`, and `.nvmrc` in every linked site directory. If any of these files change — for example after a `git checkout` to a branch with a different `.lerd.yaml` — Lerd automatically:

1. Re-detects the PHP and Node versions for the site.
2. Updates the site registry.
3. Regenerates the nginx vhost (when the PHP version changed) and reloads nginx.

No hooks or per-project setup needed — it works for every linked site out of the box.

---

## Smart defaults

| Step | Default | Condition |
|---|---|---|
| `composer install` | - [x] on | only if `vendor/` is missing |
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

The asset build step detects the right command from `package.json` — it looks for `build`, `production`, or `prod` scripts in priority order.

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
