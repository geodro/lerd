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

Available services: `mysql`, `redis`, `postgres`, `meilisearch`, `rustfs`, `mailpit`.

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

::: tip Two sets of hostnames
Services run as Podman containers on the `lerd` network. Two hostnames apply depending on where you're connecting from:

- **From host tools** (e.g. TablePlus, Redis CLI): use `127.0.0.1`
- **From your Laravel app** (PHP-FPM runs inside the `lerd` network): use container hostnames (e.g. `lerd-mysql`)

`lerd service start <name>` prints the correct `.env` variables to paste into your project.
:::

| Service | Host (host tools) | Host (Laravel `.env`) | Port | User | Password | DB |
|---|---|---|---|---|---|---|
| MySQL | 127.0.0.1 | lerd-mysql | 3306 | root | `lerd` | `lerd` |
| PostgreSQL | 127.0.0.1 | lerd-postgres | 5432 | postgres | `lerd` | `lerd` |
| Redis | 127.0.0.1 | lerd-redis | 6379 | — | — | — |
| Meilisearch | 127.0.0.1 | lerd-meilisearch | 7700 | — | — | — |
| RustFS | 127.0.0.1 | lerd-rustfs | 9000 | `lerd` | `lerdpassword` | per-site bucket |
| Mailpit SMTP | 127.0.0.1 | lerd-mailpit | 1025 | — | — | — |

Additional UIs:

- RustFS console: `http://127.0.0.1:9001`
- Mailpit web UI: `http://127.0.0.1:8025`

### RustFS — per-site buckets

RustFS is an S3-compatible object storage service (a drop-in replacement for MinIO). When `lerd env` detects it is needed (via `FILESYSTEM_DISK=s3` or `AWS_ENDPOINT` in `.env`), it automatically:

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
AWS_ENDPOINT=http://lerd-rustfs:9000
AWS_USE_PATH_STYLE_ENDPOINT=true
```

`AWS_URL` points to the public bucket URL (browser-reachable). `AWS_ENDPOINT` is the internal container address used by PHP.

### Migrating from MinIO to RustFS

RustFS exposes the same S3 API as MinIO with the same default credentials — no application changes are needed after migration.

**Automatic prompt during `lerd update`**

If lerd detects an existing MinIO data directory (`~/.local/share/lerd/data/minio`) during `lerd update`, it will offer to migrate automatically:

```
==> MinIO detected — migrate to RustFS? [y/N]
```

Answering `y` runs the full migration in-place. The update continues regardless of your answer.

**Manual migration**

```bash
lerd minio:migrate
```

This command:

1. Stops the `lerd-minio` container (if running)
2. Removes the MinIO quadlet so it no longer auto-starts
3. Copies `~/.local/share/lerd/data/minio/` → `~/.local/share/lerd/data/rustfs/`
4. Updates `~/.config/lerd/config.yaml` — removes the `minio` entry and adds `rustfs`
5. Installs and starts the `lerd-rustfs` service

The original MinIO data directory is **not deleted** — verify the migration works, then remove it manually:

```bash
rm -rf ~/.local/share/lerd/data/minio
```

---

## Service presets

Lerd ships a small set of opt-in service presets that you can install in one
command without cluttering the built-in service list. Each preset is just a
bundled YAML file that becomes a normal custom service once installed, so it
plays nicely with every `lerd service` subcommand (start/stop/remove/expose/pin).

| Preset | Image / versions | Depends on | Dashboard / host port |
|---|---|---|---|
| `phpmyadmin` | `docker.io/library/phpmyadmin:latest` | `mysql` (built-in) | `http://localhost:8080` |
| `pgadmin` | `docker.io/dpage/pgadmin4:latest` | `postgres` (built-in) | `http://localhost:8081` |
| `mysql` | `5.7` (default) / `5.6` — alternates only, the built-in `mysql` covers `8.0` | — | `127.0.0.1:3357` / `127.0.0.1:3356` |
| `mariadb` | `11` (default) / `10.11` LTS | — | `127.0.0.1:3411` / `127.0.0.1:3410` |
| `mongo` | `docker.io/library/mongo:7` | — | `127.0.0.1:27017` |
| `mongo-express` | `docker.io/library/mongo-express:latest` | `mongo` (preset) | `http://localhost:8082` |
| `selenium` | `docker.io/selenium/standalone-chromium:latest` | — | `http://localhost:7900` (noVNC) |
| `stripe-mock` | `docker.io/stripemock/stripe-mock:latest` | — | `127.0.0.1:12111` |

```bash
# List the bundled presets and their install state
lerd service preset

# Install a single-version preset
lerd service preset phpmyadmin

# Install a specific version of a multi-version preset
lerd service preset mysql --version 5.7
lerd service preset mariadb --version 10.11

# Start it (dependencies are auto-started recursively)
lerd service start phpmyadmin

# Remove it later if you no longer need it
lerd service remove phpmyadmin
```

The web UI exposes the same flow: open the **Services** tab, click the **+**
button next to the panel header, and pick a preset from the modal. Multi-version
presets like `mysql` and `mariadb` show a version dropdown next to the **Add**
button. Already-installed presets are filtered out — for multi-version
families, only the still-uninstalled versions appear.

The detail panel of every database service (built-in `mysql` / `postgres`, any
installed `mongo`, and any installed alternate like `mysql-5-7`) surfaces a
sky-blue suggestion banner offering to install the paired admin UI when it
isn't installed yet. The banner is dismissable per-preset and dismissal
persists in `localStorage`.

### Multi-version presets

`mysql` and `mariadb` ship multiple selectable versions. Each picked version
materialises as a distinct custom service whose name is `<family>-<sanitized-tag>`:

| Picked | Service name | Container | Host port | Data dir |
|---|---|---|---|---|
| `mysql 5.7` | `mysql-5-7` | `lerd-mysql-5-7` | `127.0.0.1:3357` | `~/.local/share/lerd/data/mysql-5-7/` |
| `mysql 5.6` | `mysql-5-6` | `lerd-mysql-5-6` | `127.0.0.1:3356` | `~/.local/share/lerd/data/mysql-5-6/` |
| `mariadb 11` | `mariadb-11` | `lerd-mariadb-11` | `127.0.0.1:3411` | `~/.local/share/lerd/data/mariadb-11/` |
| `mariadb 10.11` | `mariadb-10-11` | `lerd-mariadb-10-11` | `127.0.0.1:3410` | `~/.local/share/lerd/data/mariadb-10-11/` |

Each version has its own data directory so they can run side by side. The
host port is fixed per version so the same `127.0.0.1:<port>` URL works on any
machine — note that another process on the host bound to the same port will
make the alternate fail to start with a `bind: address already in use` error
in `journalctl --user -u lerd-<service>`. Use `lerd service expose <service>
<other:3306>` to add a different mapping if you hit a collision.

The mysql preset bundles a `my.cnf` (`/etc/mysql/conf.d/lerd.cnf`) that
enables `innodb_large_prefix`, `Barracuda`, `innodb_default_row_format=DYNAMIC`
(via `loose-` so MySQL 5.6 ignores it), and `innodb_strict_mode=OFF`. Combined
this lets stock Laravel migrations run on every supported version without
needing `Schema::defaultStringLength(191)` in `AppServiceProvider`.

### Service families and admin UI auto-discovery

A preset can declare a `family:` so admin UIs can find every member with one
directive. The bundled `mysql` and `mariadb` presets declare `family: mysql`
and `family: mariadb` respectively. The built-in `mysql` and `postgres`
services are members of the `mysql` and `postgres` families implicitly.

phpMyAdmin uses this with the `dynamic_env` directive:

```yaml
dynamic_env:
  PMA_HOSTS: discover_family:mysql,mariadb
```

`PMA_HOSTS` is recomputed at every quadlet generation as a comma-joined list
of every installed mysql / mariadb family member's container hostname (e.g.
`lerd-mysql,lerd-mysql-5-7,lerd-mariadb-11`). The resulting login page shows
a server dropdown with every variant; auto-login still works with the
preset's static `PMA_USER` / `PMA_PASSWORD`.

Lerd automatically regenerates phpMyAdmin's quadlet (and any other consumer
of `discover_family`) whenever a family member is **installed**, **removed**,
**started**, or **stopped**. Active consumers are stop-removed-restarted in
one shot so the new env vars take effect without DNS / connection caching
holding stale state.

### `.lerd.yaml` preset references

When a service installed via a preset is saved into a project's `.lerd.yaml`
by `lerd init`, lerd stores a **preset reference** instead of inlining the
full service definition:

```yaml
services:
  - mysql:
      preset: mysql
      version: "5.6"
  - redis
  - meilisearch
```

This keeps `.lerd.yaml` small and lets each machine resolve the embedded
preset locally — picking up any preset improvements in newer lerd versions
without churn in the project file. When a teammate clones the project and
runs `lerd link` / `lerd setup`, lerd checks whether the referenced preset
is installed locally and calls `lerd service preset <name> --version <ver>`
under the hood if it isn't.

Hand-rolled custom services that don't come from a preset still inline their
full definition into `.lerd.yaml` for portability — see [Custom services](#custom-services)
below.

### Dependency rules

A preset's `depends_on` is enforced two ways:

1. **At install time** — installing a preset whose dependency is another *custom* service (not a built-in) is rejected until the dependency is installed first. `lerd service preset mongo-express` errors out with `preset "mongo-express" requires service(s) mongo to be installed first` until you run `lerd service preset mongo`. Built-in deps (mysql, postgres) are always satisfied. The Web UI's preset picker disables the **Add** button with the same gating and shows an amber "install mongo first" hint.
2. **At start/stop time** — `lerd service start mongo-express` brings `mongo` up first, recursively. `lerd service stop mongo` first stops `mongo-express` (and any other dependent), then stops `mongo`. The Web UI's Start and Stop buttons share the same semantics. This also means starting *any* preset that depends on a built-in (`phpmyadmin`, `pgadmin`) auto-starts the database.

### Default credentials

| Preset | Sign-in |
|---|---|
| `phpmyadmin` | auto-authenticated against `lerd-mysql` as `root` / `lerd` |
| `pgadmin` | `admin@pgadmin.org` / `lerd` (server mode disabled, no master password) — pre-loaded with the `Lerd Postgres` connection via a bundled `servers.json` + `pgpass` |
| `mongo` | root user `root` / `lerd` |
| `mongo-express` | basic auth disabled — open `http://localhost:8082` directly |
| `stripe-mock` | no auth (Stripe test mock) |

### Database service quality-of-life

When a preset's paired admin UI is installed, the database service's detail
panel header gains an **Open phpMyAdmin / pgAdmin / Mongo Express** button.
Clicking it auto-starts the admin service (which in turn auto-starts the
database via `depends_on`) and opens the dashboard URL in a new tab.

When the paired admin UI is *not* installed and the service is **active**,
the header instead shows an **Open connection URL** anchor — a real `<a>`
element pointing at `mysql://`, `postgresql://`, or `mongodb://` so your
registered DB client (DBeaver, TablePlus, DataGrip, Compass…) handles it
natively. Right-click → "Copy link" works.

`mongo` declares its own `connection_url:` (see [YAML schema](#yaml-schema)
below) so it gets the same treatment as the built-in databases.

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

connection_url: mongodb://root:secret@127.0.0.1:27017/?authSource=admin
                                       # host-side scheme URL (mysql://, postgresql://, mongodb://, etc.)
                                       # Surfaced as an "Open connection URL" link on the service detail
                                       # panel when the service is active and no paired admin UI is installed.
                                       # Right-click "Copy link" works; left-click hands the URL to your
                                       # registered DB client (DBeaver, TablePlus, Compass, etc.).

description: "MongoDB document store"  # shown in `lerd service list`

# Service dependencies (see "Service dependencies" section below)
depends_on:
  - mysql                              # services that must start before this one
                                       # `lerd service start <name>` recursively starts each dep first.
                                       # `lerd service stop <name>` stops anything that depends on it first.

# Bind-mounted config files materialised on the host at install time
files:
  - target: /etc/mytool/config.yaml    # absolute path inside the container
    content: |                         # literal file body, written verbatim
      key: value
      nested:
        item: 1
  - target: /run/secret                # files needing strict perms / non-root reads
    mode: "0600"                       # octal permission bits, default "0644"
    chown: true                        # adds :U to the volume mount so podman re-chowns
                                       # the file to the container user. Required when
                                       # the in-container process runs as a non-root uid
                                       # (e.g. pgAdmin's uid 5050) and 0600 would otherwise
                                       # hide the file from it.
    content: |
      hunter2

# Files are rendered to ~/.local/share/lerd/service-files/<name>/ and bind-mounted
# at the declared target. Materialisation runs at install time and on every
# `lerd service start` so editing the YAML and restarting picks up changes.

# Family groups related services so admin UIs can auto-discover every member.
# Built-in mysql / postgres / redis / etc. are always implicitly in the family
# of the same name. Multi-version preset alternates inherit this through the
# preset YAML; hand-rolled custom services can opt in by setting the field.
family: mysql

# Dynamic env vars are computed at quadlet generation time. Currently supported
# directive: discover_family:<name>[,<name>...] which expands to a comma-joined
# list of container hostnames for every installed service in the named families.
# phpMyAdmin uses this to populate PMA_HOSTS with all mysql + mariadb variants.
dynamic_env:
  PMA_HOSTS: discover_family:mysql,mariadb

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

<!-- markdownlint-disable-next-line -->
<div v-pre>

| Placeholder | Expands to |
|---|---|
| `{{site}}` | Project site handle (derived from the registered site name or directory name, hyphens converted to underscores) |
| `{{site_testing}}` | Same as `{{site}}` with `_testing` appended |
| `{{mysql_version}}` | Major version of the MySQL service image (e.g. `8.0`) |
| `{{postgres_version}}` | Major version of the PostgreSQL service image (e.g. `16`) |
| `{{redis_version}}` | Major version of the Redis service image (e.g. `7`) |
| `{{meilisearch_version}}` | Version of the Meilisearch service image (e.g. `1.7`) |

These are not limited to database names — use them anywhere a per-project identifier is needed (a bucket name, a queue prefix, a namespace, etc.).

</div>

### How `lerd env` uses custom services

When `lerd env` runs in a project directory, it checks each custom service's `env_detect` rule against the project's `.env`. If a match is found:

1. `env_vars` are written into `.env`, with <code v-pre>{{site}}</code> and <code v-pre>{{site_testing}}</code> substituted
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

Services are shown in a two-column format optimised for narrow terminals. Custom services include a `[custom]` marker. Inactive reasons and dependency info appear as indented sub-lines:

```
Service              Status
────────────────────────────────
mysql                active
redis                inactive
  no sites using this service
phpmyadmin           active  [custom]
  depends on: mysql
```

- **no sites using this service** — the service was auto-stopped because no active site's `.env` references it
- **depends on: …** — the service has declared dependencies (see "Service dependencies" below)

### Service dependencies

Custom services can declare that they need another service to be running first using `depends_on`. Lerd uses this to automatically manage start and stop order.

**Define via YAML:**

```yaml
# ~/.config/lerd/services/phpmyadmin.yaml
name: phpmyadmin
image: docker.io/phpmyadmin:latest
ports:
  - 8080:80
depends_on:
  - mysql
dashboard: http://localhost:8080
description: "phpMyAdmin web interface for MySQL"
```

**Define via flags:**

```bash
lerd service add \
  --name phpmyadmin \
  --image docker.io/phpmyadmin:latest \
  --port 8080:80 \
  --depends-on mysql \
  --dashboard http://localhost:8080
```

**Behaviour:**

| Action | Effect |
|---|---|
| `lerd service start phpmyadmin` | Starts `mysql` first (if not already running), then starts `phpmyadmin` |
| `lerd service start mysql` | Starts `mysql`, then also starts any services that depend on it (e.g. `phpmyadmin`) |
| `lerd service stop mysql` | Stops `phpmyadmin` first (cascade), then stops `mysql` |
| Site pause (auto-stops `mysql`) | `phpmyadmin` is stopped first, then `mysql` |
| Site unpause (starts `mysql`) | `mysql` starts, then `phpmyadmin` starts |

Multiple dependencies are supported:

```yaml
depends_on:
  - mysql
  - redis
```

Dependencies can be built-in services (`mysql`, `redis`, `postgres`, `meilisearch`, `rustfs`, `mailpit`) or other custom services.

::: info
Circular dependencies (A depends on B, B depends on A) are not detected at definition time. The start cycle is naturally broken because a service already active is skipped. Avoid circular configurations.
:::

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
| `--env-var` | `.env` variable injected by `lerd env`, supports <code v-pre>{{site}}</code> — repeatable |
| `--data-dir` | Mount path inside the container for persistent data |
| `--detect-key` | `.env` key that triggers auto-detection in `lerd env` |
| `--detect-prefix` | Optional value prefix filter for auto-detection |
| `--init-exec` | Shell command run inside the container once per site (supports <code v-pre>{{site}}</code> and <code v-pre>{{site_testing}}</code>) |
| `--init-container` | Container to run `--init-exec` in (default: `lerd-<name>`) |
| `--dashboard` | URL to open when clicking the dashboard button in the web UI |
| `--description` | Description shown in `lerd service list` |
| `--depends-on` | Service name that must be running before this one — repeatable (`--depends-on mysql --depends-on redis`) |
