# Service presets

Service presets are the YAML-driven definitions for every service lerd manages. There are two kinds:

- **Default presets** (`default: true`) — the always-recognised services that ship with lerd: `mysql`, `redis`, `postgres`, `meilisearch`, `rustfs`, `mailpit`. They get auto-listed in `lerd service` everywhere; their lifecycle is identical to add-on presets but they don't need an explicit install step.
- **Add-on presets** — opt-in installers for phpMyAdmin, pgAdmin, MongoDB, alternate MySQL / MariaDB versions, Selenium, Stripe Mock, Memcached, Valkey, RabbitMQ, Soketi, Beanstalkd, Elasticsearch, OpenSearch, Typesense, Typesense Dashboard, Elasticvue, RedisInsight.

Both kinds use the same YAML schema in `internal/config/presets/*.yaml` and the same code path. Adding or replacing a default service is a YAML edit, not a code change. See [Service updates](service-updates.md) for the configuration knobs (`update_strategy`, `track_latest`, `allow_major_upgrade`).

## Default service presets

| Preset | Default image | Update strategy | Notes |
|---|---|---|---|
| `mysql` | `docker.io/library/mysql:8.4` (LTS) | `minor` (track_latest) | Multi-version: 8.4 canonical + 9.7 LTS, 5.7 alternates on host ports 3397 / 3357. SQL migration supported. |
| `postgres` | `docker.io/postgis/postgis:16-3.5-alpine` | `minor` (track_latest) | Multi-version: 16 canonical + 17, 18 alternates on host ports 5417 / 5418. SQL migration supported. |
| `redis` | `docker.io/library/redis:7-alpine` | `minor` (track_latest) | Forward-compat across 7.x patches. |
| `meilisearch` | `docker.io/getmeili/meilisearch:v1.42` | `patch` (track_latest) | Cross-minor upgrades require manual dump/restore — automated migration is **not** offered for Meilisearch (binary dump format is version-specific). |
| `rustfs` | `docker.io/rustfs/rustfs:latest` | `rolling` | S3-compatible. |
| `mailpit` | `docker.io/axllent/mailpit:latest` | `rolling` | SMTP catcher. |

## Add-on service presets

| Preset | Image / versions | Depends on | Dashboard / host port |
|---|---|---|---|
| `phpmyadmin` | `docker.io/library/phpmyadmin:latest` | `mysql` (default) | `http://localhost:8080` |
| `pgadmin` | `docker.io/dpage/pgadmin4:latest` | `postgres` (default) | `http://localhost:8081` |
| `mysql` alternates | `5.7` / `9.7` LTS (canonical 8.4 lives in the default preset) | - | `127.0.0.1:3357` / `127.0.0.1:3397` |
| `postgres` alternates | `17` / `18` (canonical 16 lives in the default preset) | - | `127.0.0.1:5417` / `127.0.0.1:5418` |
| `postgres-pgvector` | `pgvector/pgvector:pg18` (canonical) / `pg17` / `pg16` — pgvector instead of PostGIS | - | `127.0.0.1:5518` / `127.0.0.1:5517` / `127.0.0.1:5516` |
| `mariadb` | `12` / `12.3` LTS / `11.8` LTS (default) / `11.4` LTS / `10.11` LTS / `11` (legacy) | - | host ports `3412` / `3423` / `3418` / `3414` / `3410` / `3411` |
| `mongo` | `docker.io/library/mongo:7` | - | `127.0.0.1:27017` |
| `mongo-express` | `docker.io/library/mongo-express:latest` | `mongo` (preset) | `http://localhost:8082` |
| `selenium` | `docker.io/selenium/standalone-chromium:latest` | - | `http://localhost:7900` (noVNC) |
| `stripe-mock` | `docker.io/stripemock/stripe-mock:latest` | - | `127.0.0.1:12111` |
| `memcached` | `docker.io/library/memcached:1.6-alpine` | - | `127.0.0.1:11211` |
| `valkey` | `docker.io/valkey/valkey:9-alpine` | - | `127.0.0.1:6380` |
| `rabbitmq` | `docker.io/library/rabbitmq:3-management-alpine` | - | `http://localhost:15672` (mgmt UI, opens in new tab) |
| `soketi` | `quay.io/soketi/soketi:1.6-16-alpine` | - | `127.0.0.1:6001` |
| `beanstalkd` | `docker.io/schickling/beanstalkd:latest` | - | `127.0.0.1:11300` |
| `elasticsearch` | `docker.elastic.co/elasticsearch/elasticsearch:8.13.4` | - | `127.0.0.1:9200` |
| `opensearch` | `docker.io/opensearchproject/opensearch:2.19.5` | - | `127.0.0.1:9201` |
| `typesense` | `docker.io/typesense/typesense:30.2` | - | `127.0.0.1:8108` |
| `typesense-dashboard` | `docker.io/bfritscher/typesense-dashboard:latest` | `typesense` (preset) | `http://localhost:8084` |
| `elasticvue` | `docker.io/cars10/elasticvue:latest` | `elasticsearch` (preset) | `http://localhost:8083` |
| `redisinsight` | `docker.io/redis/redisinsight:latest` | `redis` (preset) | `http://localhost:8085` (opens in new tab) |

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
installed `mongo`, and any installed alternate like `mysql-5-7`), search
engine (`elasticsearch` → Elasticvue, `typesense` → Typesense Dashboard) and
`redis` → RedisInsight
surfaces a sky-blue suggestion banner offering to install the paired admin UI
when it isn't installed yet. The banner is dismissable per-preset and
dismissal persists in `localStorage`.

## Multi-version presets

`mysql`, `postgres` and `mariadb` ship multiple selectable versions. `mysql` and `postgres` mark one version canonical (mysql 8.4 LTS, postgres 16): the canonical version is the default install, recognised as the bare service `mysql` / `postgres` on the canonical host port, and the alternates picker only shows non-canonical versions (it doesn't list 8.4 because that IS the default install). `mariadb` has no canonical version, so there is no bare `mariadb` service: every version, including the default `11.8` LTS, materialises as a distinct custom service named `<family>-<sanitized-tag>` (the default resolves to `mariadb-11-8`). Non-canonical alternates run side-by-side with each other and with the canonical.

| Picked | Service name | Container | Host port | Data dir |
|---|---|---|---|---|
| `mysql 8.4` (canonical) | `mysql` | `lerd-mysql` | `127.0.0.1:3306` | `~/.local/share/lerd/data/mysql/` |
| `mysql 9.7` LTS | `mysql-9-7` | `lerd-mysql-9-7` | `127.0.0.1:3397` | `~/.local/share/lerd/data/mysql-9-7/` |
| `mysql 5.7` | `mysql-5-7` | `lerd-mysql-5-7` | `127.0.0.1:3357` | `~/.local/share/lerd/data/mysql-5-7/` |
| `postgres 16` (canonical) | `postgres` | `lerd-postgres` | `127.0.0.1:5432` | `~/.local/share/lerd/data/postgres/` |
| `postgres 17` | `postgres-17` | `lerd-postgres-17` | `127.0.0.1:5417` | `~/.local/share/lerd/data/postgres-17/` |
| `postgres 18` | `postgres-18` | `lerd-postgres-18` | `127.0.0.1:5418` | `~/.local/share/lerd/data/postgres-18/` |
| `mariadb 12` | `mariadb-12` | `lerd-mariadb-12` | `127.0.0.1:3412` | `~/.local/share/lerd/data/mariadb-12/` |
| `mariadb 12.3` LTS | `mariadb-12-3` | `lerd-mariadb-12-3` | `127.0.0.1:3423` | `~/.local/share/lerd/data/mariadb-12-3/` |
| `mariadb 11.8` LTS (default) | `mariadb-11-8` | `lerd-mariadb-11-8` | `127.0.0.1:3418` | `~/.local/share/lerd/data/mariadb-11-8/` |
| `mariadb 11.4` LTS | `mariadb-11-4` | `lerd-mariadb-11-4` | `127.0.0.1:3414` | `~/.local/share/lerd/data/mariadb-11-4/` |
| `mariadb 11` (legacy) | `mariadb-11` | `lerd-mariadb-11` | `127.0.0.1:3411` | `~/.local/share/lerd/data/mariadb-11/` |
| `mariadb 10.11` LTS | `mariadb-10-11` | `lerd-mariadb-10-11` | `127.0.0.1:3410` | `~/.local/share/lerd/data/mariadb-10-11/` |

Alternates inherit the preset's `update_strategy` but with one caveat: they're internally promoted to `patch` strategy so an alternate explicitly installed at v8.0 doesn't get auto-suggested an 8.4.x upgrade (which would cross the LTS line). Cross-line moves stay in the user's hands via the alternates picker or `lerd service migrate`.

Each version has its own data directory so they can run side by side. The
host port is fixed per version so the same `127.0.0.1:<port>` URL works on any
machine; note that another process on the host bound to the same port will
make the alternate fail to start with a `bind: address already in use` error
in `journalctl --user -u lerd-<service>`. Use `lerd service expose <service>
<other:3306>` to add a different mapping if you hit a collision.

### Canonical version pinning

Each default-preset install records which version's tag was canonical at install time in `~/.config/lerd/config.yaml` under `services.<name>.canonical_version`. That pin survives future canonical flips in the bundled YAML: when a later lerd release promotes (say) `postgres 18` to canonical, existing installs whose pin says `16` continue to resolve against 16 and keep their bare service name. New installs land on whatever the YAML currently calls canonical and get pinned to that.

`lerd service migrate <service> <tag>` is the explicit cross-major path. Beyond the dump/restore, it rewrites `canonical_version` to the new tag so the next reconcile honors the move instead of reverting to the original pin.

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
| `valkey` | no auth (no password set for local dev, same as `redis`) |
| `rabbitmq` | management UI: `root` / `lerd` (also the default AMQP user) |
| `soketi` | Pusher app id / key / secret all `lerd`, default cluster `mt1` |
| `beanstalkd` | no auth (Beanstalkd has no native authentication) |
| `elasticsearch` | no auth (`xpack.security.enabled=false` for local dev) |
| `opensearch` | no auth (security plugin disabled for local dev) |
| `typesense` | API key `lerd`, sent as the `X-TYPESENSE-API-KEY` header |
| `typesense-dashboard` | no sign-in, opens pre-connected to the lerd Typesense node at `localhost:8108` |
| `elasticvue` | no auth, opens straight to the pre-configured `Lerd Elasticsearch` cluster at `http://localhost:9200` |
| `redisinsight` | no sign-in, opens pre-wired to the lerd Redis connection at `lerd-redis:6379` |

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

## Removing and reinstalling presets

Default presets can be removed: `lerd service remove postgres` (or any other) stops the unit, deletes the quadlet, and frees the slot. The preset itself stays available in `lerd service preset list` as not-installed, so a future `lerd service preset postgres` brings it back. Pass `--purge` to also rename the data dir aside.

`lerd service reinstall <name>` stops, removes, and reinstalls at the current version. `--reset-data` wipes the data and recreates per-site state on the fresh container (databases for mysql/mariadb/postgres, buckets for rustfs). See [custom services](custom-services.md#reinstalling-a-service) for the resolution rules.
