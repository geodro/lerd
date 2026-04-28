# Service presets

Service presets are the YAML-driven definitions for every service lerd manages. There are two kinds:

- **Default presets** (`default: true`) — the always-recognised services that ship with lerd: `mysql`, `redis`, `postgres`, `meilisearch`, `rustfs`, `mailpit`. They get auto-listed in `lerd service` everywhere; their lifecycle is identical to add-on presets but they don't need an explicit install step.
- **Add-on presets** — opt-in installers for phpMyAdmin, pgAdmin, MongoDB, alternate MySQL / MariaDB versions, Selenium, Stripe Mock, Memcached, RabbitMQ, Elasticsearch, Elasticvue.

Both kinds use the same YAML schema in `internal/config/presets/*.yaml` and the same code path. Adding or replacing a default service is a YAML edit, not a code change. See [Service updates](service-updates.md) for the configuration knobs (`update_strategy`, `track_latest`, `allow_major_upgrade`).

## Default service presets

| Preset | Default image | Update strategy | Notes |
|---|---|---|---|
| `mysql` | `docker.io/library/mysql:8.4` (LTS) | `minor` (track_latest) | Multi-version: 8.4 canonical + 5.7, 8.0 alternates on host ports 3357 / 3380. SQL migration supported. |
| `postgres` | `docker.io/postgis/postgis:16-3.5-alpine` | `minor` (track_latest) | Darwin platform override → `imresamu/postgis` for ARM64 compatibility. SQL migration supported. |
| `redis` | `docker.io/library/redis:7-alpine` | `minor` (track_latest) | Forward-compat across 7.x patches. |
| `meilisearch` | `docker.io/getmeili/meilisearch:v1.42` | `patch` (track_latest) | Cross-minor upgrades require manual dump/restore — automated migration is **not** offered for Meilisearch (binary dump format is version-specific). |
| `rustfs` | `docker.io/rustfs/rustfs:latest` | `rolling` | S3-compatible. |
| `mailpit` | `docker.io/axllent/mailpit:latest` | `rolling` | SMTP catcher. |

## Add-on service presets

| Preset | Image / versions | Depends on | Dashboard / host port |
|---|---|---|---|
| `phpmyadmin` | `docker.io/library/phpmyadmin:latest` | `mysql` (default) | `http://localhost:8080` |
| `pgadmin` | `docker.io/dpage/pgadmin4:latest` | `postgres` (default) | `http://localhost:8081` |
| `mysql` alternates | `5.7` / `8.0` (canonical 8.4 lives in the default preset) | - | `127.0.0.1:3357` / `127.0.0.1:3380` |
| `mariadb` | `11` (default) / `10.11` LTS | - | `127.0.0.1:3411` / `127.0.0.1:3410` |
| `mongo` | `docker.io/library/mongo:7` | - | `127.0.0.1:27017` |
| `mongo-express` | `docker.io/library/mongo-express:latest` | `mongo` (preset) | `http://localhost:8082` |
| `selenium` | `docker.io/selenium/standalone-chromium:latest` | - | `http://localhost:7900` (noVNC) |
| `stripe-mock` | `docker.io/stripemock/stripe-mock:latest` | - | `127.0.0.1:12111` |
| `memcached` | `docker.io/library/memcached:1.6-alpine` | - | `127.0.0.1:11211` |
| `rabbitmq` | `docker.io/library/rabbitmq:3-management-alpine` | - | `http://localhost:15672` (mgmt UI, opens in new tab) |
| `elasticsearch` | `docker.elastic.co/elasticsearch/elasticsearch:8.13.4` | - | `127.0.0.1:9200` |
| `elasticvue` | `docker.io/cars10/elasticvue:latest` | `elasticsearch` (preset) | `http://localhost:8083` |

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
button. Already-installed presets are filtered out; for multi-version
families, only the still-uninstalled versions appear.

The Add button streams per-phase progress while it works, so the spinner label
tracks the real step: *Writing config…*, *Starting elasticsearch…* for each
dependency, *Pulling image…* with live podman output underneath, *Starting
service…*, then *Waiting for ready…*. Most of the perceived latency on a
first install is the image pull; the progress line shows what layer is being
copied so a slow registry is distinguishable from a stuck install.

The detail panel of every database service (built-in `mysql` / `postgres`, any
installed `mongo`, and any installed alternate like `mysql-5-7`) surfaces a
sky-blue suggestion banner offering to install the paired admin UI when it
isn't installed yet. The banner is dismissable per-preset and dismissal
persists in `localStorage`.

## Multi-version presets

`mysql` and `mariadb` ship multiple selectable versions. The canonical version (mysql 8.4 LTS, mariadb 11) is the default install — recognised as the bare service `mysql` / `mariadb` on the canonical host port. Non-canonical alternates materialise as distinct custom services named `<family>-<sanitized-tag>`, runnable side-by-side with the canonical. The alternates picker only shows non-canonical versions (it doesn't list 8.4 because that IS the default install).

| Picked | Service name | Container | Host port | Data dir |
|---|---|---|---|---|
| `mysql 8.4` (canonical) | `mysql` | `lerd-mysql` | `127.0.0.1:3306` | `~/.local/share/lerd/data/mysql/` |
| `mysql 8.0` | `mysql-8-0` | `lerd-mysql-8-0` | `127.0.0.1:3380` | `~/.local/share/lerd/data/mysql-8-0/` |
| `mysql 5.7` | `mysql-5-7` | `lerd-mysql-5-7` | `127.0.0.1:3357` | `~/.local/share/lerd/data/mysql-5-7/` |
| `mariadb 11` (canonical) | `mariadb-11` | `lerd-mariadb-11` | `127.0.0.1:3411` | `~/.local/share/lerd/data/mariadb-11/` |
| `mariadb 10.11` | `mariadb-10-11` | `lerd-mariadb-10-11` | `127.0.0.1:3410` | `~/.local/share/lerd/data/mariadb-10-11/` |

Alternates inherit the preset's `update_strategy` but with one caveat: they're internally promoted to `patch` strategy so an alternate explicitly installed at v8.0 doesn't get auto-suggested an 8.4.x upgrade (which would cross the LTS line). Cross-line moves stay in the user's hands via the alternates picker or `lerd service migrate`.

Each version has its own data directory so they can run side by side. The
host port is fixed per version so the same `127.0.0.1:<port>` URL works on any
machine; note that another process on the host bound to the same port will
make the alternate fail to start with a `bind: address already in use` error
in `journalctl --user -u lerd-<service>`. Use `lerd service expose <service>
<other:3306>` to add a different mapping if you hit a collision.

The mysql preset bundles a `my.cnf` (`/etc/mysql/conf.d/lerd.cnf`) that
enables `innodb_large_prefix`, `Barracuda`, `innodb_default_row_format=DYNAMIC`
(via `loose-` so MySQL 5.6 ignores it), and `innodb_strict_mode=OFF`. Combined
this lets stock Laravel migrations run on every supported version without
needing `Schema::defaultStringLength(191)` in `AppServiceProvider`.

## Service families and admin UI auto-discovery

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

## `.lerd.yaml` preset references

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
preset locally, picking up any preset improvements in newer lerd versions
without churn in the project file. When a teammate clones the project and
runs `lerd link` / `lerd setup`, lerd checks whether the referenced preset
is installed locally and calls `lerd service preset <name> --version <ver>`
under the hood if it isn't.

Hand-rolled custom services that don't come from a preset still inline their
full definition into `.lerd.yaml` for portability; see [Custom services](custom-services.md).

## Dependency rules

A preset's `depends_on` is enforced two ways:

1. **At install time**: installing a preset whose dependency is another *custom* service (not a built-in) is rejected until the dependency is installed first. `lerd service preset mongo-express` errors out with `preset "mongo-express" requires service(s) mongo to be installed first` until you run `lerd service preset mongo`. Built-in deps (mysql, postgres) are always satisfied. The Web UI's preset picker disables the **Add** button with the same gating and shows an amber "install mongo first" hint.
2. **At start/stop time**: `lerd service start mongo-express` brings `mongo` up first, recursively. `lerd service stop mongo` first stops `mongo-express` (and any other dependent), then stops `mongo`. The Web UI's Start and Stop buttons share the same semantics. This also means starting *any* preset that depends on a built-in (`phpmyadmin`, `pgadmin`) auto-starts the database.

## Default credentials

| Preset | Sign-in |
|---|---|
| `phpmyadmin` | auto-authenticated against `lerd-mysql` as `root` / `lerd` |
| `pgadmin` | `admin@pgadmin.org` / `lerd` (server mode disabled, no master password), pre-loaded with the `Lerd Postgres` connection via a bundled `servers.json` + `pgpass` |
| `mongo` | root user `root` / `lerd` |
| `mongo-express` | basic auth disabled, open `http://localhost:8082` directly |
| `stripe-mock` | no auth (Stripe test mock) |
| `memcached` | no auth (Memcached has no native authentication) |
| `rabbitmq` | management UI: `root` / `lerd` (also the default AMQP user) |
| `elasticsearch` | no auth (`xpack.security.enabled=false` for local dev) |
| `elasticvue` | no auth, opens straight to the pre-configured `Lerd Elasticsearch` cluster at `http://localhost:9200` |

## Database service quality-of-life

When a preset's paired admin UI is installed, the database service's detail
panel header gains an **Open phpMyAdmin / pgAdmin / Mongo Express** button.
Clicking it auto-starts the admin service (which in turn auto-starts the
database via `depends_on`) and opens the dashboard URL in a new tab.

When the paired admin UI is *not* installed and the service is **active**,
the header instead shows an **Open connection URL** anchor, a real `<a>`
element pointing at `mysql://`, `postgresql://`, or `mongodb://` so your
registered DB client (DBeaver, TablePlus, DataGrip, Compass, etc.) handles it
natively. Right-click "Copy link" works.

`mongo` declares its own `connection_url:` (see [YAML schema](custom-services.md#yaml-schema)
in the custom services reference) so it gets the same treatment as the built-in databases.
