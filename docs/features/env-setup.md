# Environment Setup

`lerd env` sets up the `.env` file for a Laravel project in one command:

```bash
cd ~/Lerd/my-app
lerd env
```

---

## What it does

1. **Creates the env file** from the framework's example file (e.g. `.env.example` for Laravel, `.env.dist` for Symfony) if no env file exists yet
2. **Detects which services the project uses**: for Laravel, by inspecting env keys (`DB_CONNECTION`, `REDIS_HOST`, etc.); for other frameworks, using the service detection rules defined in the [framework definition](../usage/frameworks.md). Services listed in `.lerd.yaml` are also included even when the env file does not reference them yet
3. **Writes lerd connection values** for each detected service (hosts, ports, credentials), preserving all comments and line order
4. **Creates the project database** (and a `<name>_testing` database) inside the running service container; reports if they already exist
5. **Starts any referenced service** that is not already running
6. **Sets the app URL** (`APP_URL` for Laravel; the `url_key` defined in the framework for others) to the project's registered `.test` domain
7. **Generates `APP_KEY`** if the key is missing or empty (Laravel only). Uses `php artisan key:generate` when `vendor/` is installed; on a fresh project before `composer install`, writes a random base64 key directly so post-install scripts can boot without a `MissingAppKeyException`
8. **Generates `REVERB_*` values**: if `BROADCAST_CONNECTION=reverb` is detected, generates `REVERB_APP_ID`, `REVERB_APP_KEY`, `REVERB_APP_SECRET` using random secure values for secrets. Also assigns a unique `REVERB_SERVER_PORT` so multiple Reverb-enabled sites can run simultaneously without port collisions (starts at `8080`, increments per site). `REVERB_HOST`, `REVERB_PORT`, and `REVERB_SCHEME` are set to `localhost`, the assigned server port, and `http` respectively, since the queue worker runs inside the PHP-FPM container alongside Reverb and connects directly. `VITE_REVERB_HOST`, `VITE_REVERB_PORT`, and `VITE_REVERB_SCHEME` are set to the site's domain and external port so the browser connects through the nginx WebSocket proxy

---

## Example output

```
Creating .env from .env.example...
  Detected mysql:        applying lerd connection values
  Detected redis:        applying lerd connection values
  From .lerd.yaml mailpit: applying lerd connection values
  Setting APP_URL=http://my-app.test
  Generating APP_KEY...
Done.
```

Services prefixed with `From .lerd.yaml` were not referenced in the env file but are listed in `.lerd.yaml`. Their connection values are written and the service is started so the project is ready to use them.

---

## Automatic backup

The first time `lerd env` modifies an existing `.env` that has not yet been touched by lerd, it saves a copy as `.env.before_lerd` in the project root and adds the file to `.gitignore` (if one exists). Once lerd has written its connection values to `.env`, subsequent runs skip the backup entirely, so deleting `.env.before_lerd` is safe and it will never be recreated.

The backup lets you:
- **See exactly what lerd changed**: diff `.env.before_lerd` against `.env`
- **Restore the original** with `lerd env:restore` (see below)
- **Preserve original Sail credentials**: `lerd import sail` reads S3 configuration from `.env.before_lerd` when it exists, so the original Sail MinIO bucket and endpoint are used even after lerd has overwritten `.env`

Example output when the backup is created:

```
Updating existing .env...
  Backed up original .env to .env.before_lerd
  Detected mysql:        applying lerd connection values
  ...
Done.
```

---

## Restoring the original .env

```bash
lerd env:restore
```

Copies `.env.before_lerd` back to `.env`. Useful when switching a project back to Laravel Sail or another local environment.

After restoring, run `lerd env` again to re-apply lerd connection values.

---

## Personal overrides (`.env.lerd_override`)

`lerd env` writes lerd's own defaults into `.env` (for example `DB_USERNAME=lerd`). When you want different values on your machine without hand-editing `.env` after every run, drop them into a personal `.env.lerd_override` file in the project root. It is plain dotenv syntax, it is never committed (lerd adds it to `.gitignore`), and every `KEY=VALUE` in it is layered on **last** when you run `lerd env`, winning over lerd's defaults and every computed value (`DB_DATABASE`, `APP_URL`, and so on).

```bash
lerd env:override DB_USERNAME=postgres DB_PASSWORD=secret
lerd env
```

`lerd env:override` creates the file from a commented template (and gitignores it) on first run, and seeds any `KEY=VALUE` arguments you pass. Run it with no arguments to just scaffold the file, then edit it by hand. The file is the source of truth, so you can also create or edit `.env.lerd_override` directly (or in the dashboard's Env tab); whenever it is present, `lerd env` keeps it listed in `.gitignore`. Values are written into `.env` verbatim, so quote any value that needs it (spaces, `#`), e.g. `DB_PASSWORD="p@ss word"`. Because it is a partial overlay rather than a full env file, `lerd env:check` skips it.

### Using your own service instead of lerd's container

The one reserved key, `LERD_EXTERNAL_SERVICES`, lists services lerd should treat as externally managed (you run your own instance). For each named service lerd still writes its connection variables into `.env`, but it does **not** start the container and does **not** create the project database or S3 bucket. Combine it with the connection overrides that point at your own instance:

```dotenv
# .env.lerd_override — point this project at my host postgres
DB_HOST=127.0.0.1
DB_PORT=5432
DB_USERNAME=postgres
DB_PASSWORD=mysecret

LERD_EXTERNAL_SERVICES=postgres
```

The value is comma or space separated, so `LERD_EXTERNAL_SERVICES=postgres, redis` opts both out. The reserved key is consumed by lerd and is never written into `.env`.

Example output with an override file present:

```
Updating existing .env...
  postgres     externally managed (.env.lerd_override) — not starting it
  Detected redis:        applying lerd connection values
  Applying 5 override(s) from .env.lerd_override
  Setting APP_URL=http://my-app.test
Done.
```

---

## Editing .env in the dashboard

The site detail panel has an **Env** tab that opens the project's env files in an inline editor with line numbers and dotenv syntax highlighting (`KEY`, comments, quoted values). On a worktree the editor opens that worktree's files, not the parent's.

A dropdown at the start of the toolbar lists every env file the project has (`.env`, `.env.local`, `.env.testing`, `.env.example`, `.env.production`, anything matching `^\.env(\.[A-Za-z][\w-]*)?$`). Our own timestamped backups, temp files, and `.env.before_lerd` never appear in the dropdown. Pick the file you want to edit; the editor, save, and revert flows all scope to it.

Edits stay client-side until you click **Save**, which opens a confirmation modal with a single checkbox: **Back up the current file first**. The box is unchecked by default; tick it to have lerd copy the current contents to `<file>.bkp.<YYYYMMDD-HHMMSS>` in the same directory before the new file lands. Each env file has its own backups, so `.env.testing.bkp.20260528-103045` belongs only to `.env.testing` and won't appear when you have `.env` open.

The save preserves the file mode of the existing file, including permissions narrower than `0644` such as `0600`. New files default to `0644`. The write is atomic (staged temp file + rename), so a partial failure leaves the previous file (and the timestamped backup, when requested) intact.

The **Revert** button rolls back the most recent backup for the active file. When a backup exists, clicking Revert opens a diff modal showing exactly what restoring will change (removed lines from the current file marked `-`, lines coming back from the backup marked `+`). Accept and the backup is copied over the file and removed. Repeated Revert clicks peel backups off newest-first. When no backup exists, Revert simply discards unsaved edits.

The endpoint is loopback only, the same gate that protects the env reader and `terminal`, so a LAN client cannot edit a project's env files even when remote-control is enabled with valid credentials.

---

## Safe to re-run

Running `lerd env` on a project that already has a `.env` is safe; it only updates connection-related keys and leaves everything else untouched.

---

## Checking for drift

`lerd env:check` compares all `.env` files in the project against `.env.example` and shows a single table of any keys that are out of sync:

```bash
lerd env:check
```

Example output when keys are out of sync:

```
  KEY              .env.example  .env  .env.testing
  ---------------  ------------  ----  ------------
  STRIPE_KEY            ✓         ✗         ✗
  STRIPE_SECRET         ✓         ✗         ✗
  LEGACY_TOKEN          ✗         ✓         ✗

  3 key(s) out of sync
```

When everything is in sync:

```
  all .env files are in sync with .env.example
```

The command automatically discovers all `.env*` files in the project directory (`.env`, `.env.testing`, `.env.local`, etc.) and checks each one against `.env.example`. Only keys that differ in at least one file are shown.

When called via the MCP server (AI assistants), `env_check` returns structured JSON instead of the formatted table:

```json
{
  "in_sync": false,
  "keys": [
    {"key": "STRIPE_KEY", "in_example": true, "files": {".env": false, ".env.testing": false}},
    {"key": "LEGACY_TOKEN", "in_example": false, "files": {".env": true, ".env.testing": false}}
  ],
  "out_of_sync_count": 3
}
```
