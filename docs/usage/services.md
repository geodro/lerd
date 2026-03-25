# Services

## Built-in services

| Command | Description |
|---|---|
| `lerd service start <name>` | Start a service (auto-installs on first use) |
| `lerd service stop <name>` | Stop a service container |
| `lerd service restart <name>` | Restart a service container |
| `lerd service status <name>` | Show systemd unit status |
| `lerd service list` | Show all services and their current state |
| `lerd service pin <name>` | Pin a service so it is never auto-stopped |
| `lerd service unpin <name>` | Unpin a service so it can be auto-stopped when unused |
| `lerd service expose <name> <host:container>` | Publish an extra port on a built-in service |
| `lerd service expose <name> <host:container> --remove` | Remove a previously exposed port |

Available services: `mysql`, `redis`, `postgres`, `meilisearch`, `minio`, `mailpit`.

### Exposing extra ports on built-in services

Built-in services publish a fixed set of ports by default. Use `lerd service expose` to bind additional host ports without recompiling or replacing the service:

```bash
# Expose MySQL on an extra port (e.g. for a second GUI client using a different port)
lerd service expose mysql 13306:3306

# Remove the extra port
lerd service expose mysql --remove 13306:3306
```

Extra port mappings are persisted in `~/.config/lerd/config.yaml` under `services.<name>.extra_ports` and are applied automatically every time the service starts. If the service is already running when you run `expose`, it is restarted immediately to apply the change.

You can also edit `~/.config/lerd/config.yaml` directly:

```yaml
services:
  mysql:
    extra_ports:
      - "13306:3306"
```

Then apply with `lerd service restart mysql`.

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
| MinIO | 127.0.0.1 | lerd-minio | 9000 | `lerd` | `lerdpassword` | per-site bucket |
| Mailpit SMTP | 127.0.0.1 | lerd-mailpit | 1025 | — | — | — |

Additional UIs:

- MinIO console: `http://127.0.0.1:9001`
- Mailpit web UI: `http://127.0.0.1:8025`

### MinIO — per-site buckets

When `lerd env` detects MinIO (via `FILESYSTEM_DISK=s3` or `AWS_ENDPOINT` in `.env`), it automatically:

1. Creates a bucket named after the site handle (e.g. `my_project`)
2. Sets the bucket to **public access** (suitable for local development)
3. Writes the correct `.env` values:

```ini
FILESYSTEM_DISK=s3
AWS_ACCESS_KEY_ID=lerd
AWS_SECRET_ACCESS_KEY=lerdpassword
AWS_DEFAULT_REGION=us-east-1
AWS_BUCKET=my_project
AWS_URL=http://localhost:9000/my_project
AWS_ENDPOINT=http://lerd-minio:9000
AWS_USE_PATH_STYLE_ENDPOINT=true
```

`AWS_URL` points to the public bucket URL (browser-reachable). `AWS_ENDPOINT` is the internal container address used by PHP.

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

### Pinning services

By default, lerd can auto-stop services that no active site references in its `.env`. Use `pin` to keep a service running regardless of which sites are active:

```bash
lerd service pin mysql    # always keep MySQL running
lerd service pin redis
```

Pinning a service also starts it immediately if it is not already running. Unpin to restore normal auto-stop behaviour:

```bash
lerd service unpin mysql
```

Pinned services are shown with a `[pinned]` note in `lerd service list` and the web UI.

### Manually stopped services

If you stop a service with `lerd service stop` (or via the web UI), lerd records it as **manually paused**. `lerd start` and autostart on login will skip it — the service stays stopped until you explicitly start it again.

`lerd stop` + `lerd start` restores the previous state: services that were running before `lerd stop` start again; services you had manually stopped remain stopped.

### `lerd service list` output

Custom services appear after built-in services with a `[custom]` type marker. Inactive services show a short reason explaining why:

```
Service              Type       Status
──────────────────── ────────── ──────────
mysql                [builtin]  active
redis                [builtin]  inactive   (no sites using this service)
postgres             [builtin]  inactive   (start with: lerd service start postgres)
...
mongodb              [custom]   active
```

- **no sites using this service** — the service was auto-stopped because no active site's `.env` references it
- **start with: lerd service start \<name\>** — the service was manually stopped or has never been started

### Example: Soketi (Pusher-compatible WebSocket server)

Soketi is a self-hosted Pusher-compatible WebSocket server. Use this if you prefer a standalone container over Laravel Reverb.

```yaml
# ~/.config/lerd/services/soketi.yaml
name: soketi
image: quay.io/soketi/soketi:latest-16-alpine
description: "Pusher-compatible WebSocket server"
ports:
  - 6001:6001
  - 9601:9601
environment:
  SOKETI_DEFAULT_APP_ID: lerd
  SOKETI_DEFAULT_APP_KEY: lerd-key
  SOKETI_DEFAULT_APP_SECRET: lerd-secret
env_vars:
  - BROADCAST_CONNECTION=pusher
  - PUSHER_APP_ID=lerd
  - PUSHER_APP_KEY=lerd-key
  - PUSHER_APP_SECRET=lerd-secret
  - PUSHER_HOST=lerd-soketi
  - PUSHER_PORT=6001
  - PUSHER_SCHEME=http
  - PUSHER_APP_CLUSTER=mt1
  - VITE_PUSHER_APP_KEY="${PUSHER_APP_KEY}"
  - VITE_PUSHER_HOST="${PUSHER_HOST}"
  - VITE_PUSHER_PORT="${PUSHER_PORT}"
  - VITE_PUSHER_SCHEME="${PUSHER_SCHEME}"
  - VITE_PUSHER_APP_CLUSTER="${PUSHER_APP_CLUSTER}"
env_detect:
  key: PUSHER_HOST
  value_prefix: "lerd-soketi"
dashboard: http://127.0.0.1:9601
```

```bash
lerd service add ~/.config/lerd/services/soketi.yaml
lerd service start soketi
```

Soketi metrics UI: `http://127.0.0.1:9601`

---

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
