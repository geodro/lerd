# Service presets

The built-in services (MySQL, Postgres, Redis, etc.) live in [Services](services.md). This page covers the opt-in **service presets** Lerd ships: one-command installers for phpMyAdmin, pgAdmin, MongoDB, alternate MySQL / MariaDB versions, Selenium, and Stripe Mock.

Lerd ships a small set of opt-in service presets that you can install in one
command without cluttering the built-in service list. Each preset is just a
bundled YAML file that becomes a normal custom service once installed, so it
plays nicely with every `lerd service` subcommand (start/stop/remove/expose/pin).

| Preset | Image / versions | Depends on | Dashboard / host port |
|---|---|---|---|
| `phpmyadmin` | `docker.io/library/phpmyadmin:latest` | `mysql` (built-in) | `http://localhost:8080` |
| `pgadmin` | `docker.io/dpage/pgadmin4:latest` | `postgres` (built-in) | `http://localhost:8081` |
| `mysql` | `5.7` (default) / `5.6`; alternates only, the built-in `mysql` covers `8.0` | - | `127.0.0.1:3357` / `127.0.0.1:3356` |
| `mariadb` | `11` (default) / `10.11` LTS | - | `127.0.0.1:3411` / `127.0.0.1:3410` |
| `mongo` | `docker.io/library/mongo:7` | - | `127.0.0.1:27017` |
| `mongo-express` | `docker.io/library/mongo-express:latest` | `mongo` (preset) | `http://localhost:8082` |
| `selenium` | `docker.io/selenium/standalone-chromium:latest` | - | `http://localhost:7900` (noVNC) |
| `stripe-mock` | `docker.io/stripemock/stripe-mock:latest` | - | `127.0.0.1:12111` |

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

The detail panel of every database service (built-in `mysql` / `postgres`, any
installed `mongo`, and any installed alternate like `mysql-5-7`) surfaces a
sky-blue suggestion banner offering to install the paired admin UI when it
isn't installed yet. The banner is dismissable per-preset and dismissal
persists in `localStorage`.

## Multi-version presets

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
