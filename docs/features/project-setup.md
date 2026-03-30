# Project Setup

`lerd setup` automates the standard steps for getting a fresh PHP project running locally. Run it from the project root:

```bash
cd ~/Projects/my-app
lerd setup
```

Before the step selector, `lerd setup` runs the **lerd init wizard** — you choose the PHP version, HTTPS, and required services. The answers are saved to `.lerd.yaml` in the project root. Commit this file so on any future machine `lerd setup` reads it and skips the wizard entirely.

```
→ Configuring site...
? PHP version: 8.4
? Enable HTTPS? No
? Services needed:  [mysql, redis]
Saved .lerd.yaml
Linked: my-app -> my-app.test (PHP 8.4, Node 22, Framework: laravel)
```

After the wizard, a checkbox list appears with all available steps pre-selected based on the current project state:

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
