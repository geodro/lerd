# Environment Setup

`lerd env` sets up the `.env` file for a Laravel project in one command:

```bash
cd ~/Lerd/my-app
lerd env
```

---

## What it does

1. **Creates the env file** from the framework's example file (e.g. `.env.example` for Laravel, `.env.dist` for Symfony) if no env file exists yet
2. **Detects which services the project uses** — for Laravel, by inspecting env keys (`DB_CONNECTION`, `REDIS_HOST`, etc.); for other frameworks, using the service detection rules defined in the [framework definition](../usage/frameworks.md). Services listed in `.lerd.yaml` are also included even when the env file does not reference them yet
3. **Writes lerd connection values** for each detected service (hosts, ports, credentials) — preserving all comments and line order
4. **Creates the project database** (and a `<name>_testing` database) inside the running service container; reports if they already exist
5. **Starts any referenced service** that is not already running
6. **Sets the app URL** (`APP_URL` for Laravel; the `url_key` defined in the framework for others) to the project's registered `.test` domain
7. **Generates `APP_KEY`** via `php artisan key:generate` if the key is missing or empty (Laravel only)
8. **Generates `REVERB_*` values** — if `BROADCAST_CONNECTION=reverb` is detected, generates `REVERB_APP_ID`, `REVERB_APP_KEY`, `REVERB_APP_SECRET`, `REVERB_HOST`, `REVERB_PORT`, and `REVERB_SCHEME` using random secure values for secrets. Also assigns a unique `REVERB_SERVER_PORT` so multiple Reverb-enabled sites can run simultaneously without port collisions (starts at `8080`, increments per site)

---

## Example output

```
Creating .env from .env.example...
  Detected mysql        — applying lerd connection values
  Detected redis        — applying lerd connection values
  From .lerd.yaml mailpit — applying lerd connection values
  Setting APP_URL=http://my-app.test
  Generating APP_KEY...
Done.
```

Services prefixed with `From .lerd.yaml` were not referenced in the env file but are listed in `.lerd.yaml` — their connection values are written and the service is started so the project is ready to use them.

---

## Safe to re-run

Running `lerd env` on a project that already has a `.env` is safe — it only updates connection-related keys and leaves everything else untouched.
