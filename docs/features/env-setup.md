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
