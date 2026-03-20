# Services

## Built-in services

| Command | Description |
|---|---|
| `lerd service start <name>` | Start a service (auto-installs on first use) |
| `lerd service stop <name>` | Stop a service container |
| `lerd service restart <name>` | Restart a service container |
| `lerd service status <name>` | Show systemd unit status |
| `lerd service list` | Show all services and their current state |

Available services: `mysql`, `redis`, `postgres`, `meilisearch`, `minio`, `mailpit`, `soketi`.

---

## Service credentials

!!! tip "Two sets of hostnames"
    Services run as Podman containers on the `lerd` network. Two hostnames apply depending on where you're connecting from:

    - **From host tools** (e.g. TablePlus, Redis CLI): use `127.0.0.1`
    - **From your Laravel app** (PHP-FPM runs inside the `lerd` network): use container hostnames (e.g. `lerd-mysql`)

    `lerd service start <name>` prints the correct `.env` variables to paste into your project.

| Service | Host (host tools) | Host (Laravel `.env`) | Port | User | Password | DB |
|---|---|---|---|---|---|---|
| MySQL | 127.0.0.1 | lerd-mysql | 3306 | root | `lerd` | `lerd` |
| PostgreSQL | 127.0.0.1 | lerd-postgres | 5432 | postgres | `lerd` | `lerd` |
| Redis | 127.0.0.1 | lerd-redis | 6379 | — | — | — |
| Meilisearch | 127.0.0.1 | lerd-meilisearch | 7700 | — | — | — |
| MinIO | 127.0.0.1 | lerd-minio | 9000 | `lerd` | `lerdpassword` | — |
| Mailpit SMTP | 127.0.0.1 | lerd-mailpit | 1025 | — | — | — |
| Soketi | 127.0.0.1 | lerd-soketi | 6001 | — | — | — |

Additional UIs:

- MinIO console: `http://127.0.0.1:9001`
- Mailpit web UI: `http://127.0.0.1:8025`
- Soketi metrics: `http://127.0.0.1:9601`

---

## Custom services

Lerd lets you define arbitrary OCI-based services that integrate seamlessly with `lerd service`, `lerd start`/`stop`, and `lerd env` — without recompiling.

Custom service configs live at `~/.config/lerd/services/<name>.yaml`.

### Adding a custom service

**From a YAML file** (recommended for reuse or sharing):

```bash
lerd service add mongodb.yaml
```

**With flags** (quick one-off):

```bash
lerd service add \
  --name mongodb \
  --image docker.io/library/mongo:7 \
  --port 27017:27017 \
  --env MONGO_INITDB_ROOT_USERNAME=root \
  --env MONGO_INITDB_ROOT_PASSWORD=secret \
  --data-dir /data/db \
  --env-var "MONGO_DATABASE={{site}}" \
  --env-var "MONGO_URI=mongodb://root:secret@lerd-mongodb:27017/{{site}}" \
  --detect-key MONGO_URI \
  --init-exec "mongosh admin -u root -p secret --eval \"db.getSiblingDB('{{site}}').createCollection('_init')\""
```

### Removing a custom service

```bash
lerd service remove mongodb
```

This stops the container, removes the quadlet and config file. **Data at `~/.local/share/lerd/data/mongodb/` is not deleted** — remove it manually if you no longer need it.

### YAML schema

```yaml
# Required
name: mongodb                          # slug [a-z0-9-], must match filename stem
image: docker.io/library/mongo:7

# Optional
ports:
  - 27017:27017                        # host:container

environment:                           # container environment variables
  MONGO_INITDB_ROOT_USERNAME: root
  MONGO_INITDB_ROOT_PASSWORD: secret

data_dir: /data/db                     # mount target inside container
                                       # host path: ~/.local/share/lerd/data/<name>/
                                       # omit to disable persistent storage

exec: ""                               # container command override

dashboard: http://localhost:8081       # URL shown as an "Open" button in the web UI
                                       # when the service is active

description: "MongoDB document store"  # shown in `lerd service list`

# Injected into .env by `lerd env`
env_vars:
  - MONGO_DATABASE={{site}}
  - MONGO_URI=mongodb://root:secret@lerd-mongodb:27017/{{site}}

# Auto-detection for `lerd env`
env_detect:
  key: MONGO_URI                       # trigger if this key exists in .env
  value_prefix: "mongodb://"          # optional: only match if value starts with this

# Per-site initialisation run by `lerd env` after the service starts
site_init:
  container: lerd-mongodb              # optional, defaults to lerd-<name>
  exec: >
    mongosh admin -u root -p secret --eval
    "db.getSiblingDB('{{site}}').createCollection('_init');
     db.getSiblingDB('{{site_testing}}').createCollection('_init')"
```

### Site handle placeholders

`env_vars` values and `site_init.exec` support two placeholders that are substituted per-project when `lerd env` runs:

| Placeholder | Expands to |
|---|---|
| `{{site}}` | Project site handle (derived from the registered site name or directory name, hyphens converted to underscores) |
| `{{site_testing}}` | Same as `{{site}}` with `_testing` appended |

These are not limited to database names — use them anywhere a per-project identifier is needed (a bucket name, a queue prefix, a namespace, etc.).

### How `lerd env` uses custom services

When `lerd env` runs in a project directory, it checks each custom service's `env_detect` rule against the project's `.env`. If a match is found:

1. `env_vars` are written into `.env`, with `{{site}}` and `{{site_testing}}` substituted
2. The service is started if not already running
3. `site_init.exec` is run inside the container (if defined)

### How `lerd start` / `lerd stop` handle custom services

`lerd start` and `lerd stop` include any custom service that has a quadlet file installed (i.e. has been started at least once via `lerd service start`). They are started and stopped alongside the built-in services.

### `lerd service list` output

Custom services appear after built-in services with a `[custom]` type marker:

```
Service              Type       Status
──────────────────── ────────── ──────────
mysql                [builtin]  inactive
redis                [builtin]  inactive
...
mongodb              [custom]   active
```

### Example: Stripe (Laravel Cashier)

Two services cover the typical Cashier local dev workflow:

**stripe-mock** — a local Stripe API mock. No Stripe account needed. Use this for feature tests that exercise Cashier without hitting the real API.

```yaml
# ~/.config/lerd/services/stripe-mock.yaml
name: stripe-mock
image: docker.io/stripemock/stripe-mock:latest
description: "Local Stripe API mock for Cashier testing"
ports:
  - 12111:12111
```

```bash
lerd service add ~/.config/lerd/services/stripe-mock.yaml
lerd service start stripe-mock
```

Point the Stripe PHP SDK at the mock in your `AppServiceProvider` or test bootstrap:

```php
\Stripe\Stripe::$apiBase = 'http://lerd-stripe-mock:12111';
```

**stripe:listen** — forwards live/test webhook events from Stripe to your local app via the Stripe CLI. Requires a real Stripe API key.

```bash
lerd stripe:listen                        # forwards to https://myapp.test/stripe/webhook
lerd stripe:listen --path /webhooks/stripe
```

### Flag reference

| Flag | Description |
|---|---|
| `--name` | Service name, slug format `[a-z0-9-]` (required) |
| `--image` | OCI image reference (required) |
| `--port` | Port mapping `host:container` — repeatable |
| `--env` | Container environment variable `KEY=VALUE` — repeatable |
| `--env-var` | `.env` variable injected by `lerd env`, supports `{{site}}` — repeatable |
| `--data-dir` | Mount path inside the container for persistent data |
| `--detect-key` | `.env` key that triggers auto-detection in `lerd env` |
| `--detect-prefix` | Optional value prefix filter for auto-detection |
| `--init-exec` | Shell command run inside the container once per site (supports `{{site}}` and `{{site_testing}}`) |
| `--init-container` | Container to run `--init-exec` in (default: `lerd-<name>`) |
| `--dashboard` | URL to open when clicking the dashboard button in the web UI |
| `--description` | Description shown in `lerd service list` |
