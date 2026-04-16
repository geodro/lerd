# Configuration

## Global config — `~/.config/lerd/config.yaml`

Created automatically on first run with sensible defaults:

```yaml
php:
  default_version: "8.5"
node:
  default_version: "22"
nginx:
  http_port: 80
  https_port: 443
dns:
  tld: "test"
parked_directories:
  - ~/Lerd
services:
  mysql:       { enabled: true,  image: "docker.io/library/mysql:8.0",             port: 3306 }
  redis:       { enabled: true,  image: "docker.io/library/redis:7-alpine",        port: 6379 }
  postgres:    { enabled: false, image: "docker.io/postgis/postgis:16-3.5-alpine", port: 5432 }
  meilisearch: { enabled: false, image: "docker.io/getmeili/meilisearch:v1.7",     port: 7700 }
  rustfs:      { enabled: false, image: "docker.io/rustfs/rustfs:latest",          port: 9000 }
  mailpit:     { enabled: false, image: "docker.io/axllent/mailpit:latest",        port: 1025 }
```

---

## Per-project config — `.lerd.yaml`

A portable, self-contained description of a project's local environment. Created by `lerd init` or written manually, committed to the repository, and applied automatically by `lerd link` and `lerd init`.

### Fields

| Field | Description |
|---|---|
| `php_version` | PHP version for this project — highest priority, overrides `.php-version` and `composer.json` |
| `node_version` | Node version — highest priority, overrides `.nvmrc`, `.node-version`, and `package.json`; writes `.node-version` on apply if the file does not already exist |
| `framework` | Framework name — overrides auto-detection |
| `framework_def` | Full framework definition — embedded automatically for custom (non-Laravel) frameworks so the project is portable across machines |
| `secured` | When `true`, HTTPS is enabled on apply |
| `domains` | Site hostnames without the TLD (e.g. `[myapp, api]`). The first entry is the primary; additional entries become aliases. Conflict-filtered domains stay in this list on disk but are not registered |
| `app_url` | Override for `APP_URL` (or the framework's URL key) written to `.env`. Highest priority — beats the per-machine `sites.yaml` override and the default `<scheme>://<primary-domain>` generator. Use for custom path prefixes, ports, or unrelated hostnames you want shared across machines |
| `services` | Services to start on apply. Accepts built-in names, custom service names, or full inline definitions |
| `workers` | Active worker names for the site (e.g. `queue`, `horizon`, `schedule`, `reverb`, `stripe`). Automatically kept in sync by start/stop commands. Used by `lerd start` to restore workers after reinstall |
| `container` | Custom container config for non-PHP sites. When present, lerd builds a dedicated container from the project's Containerfile and nginx reverse-proxies to it. See below and [Custom Containers](../features/custom-containers.md) |
| `custom_workers` | Custom worker definitions (name to config map). Works for both PHP and custom container sites. See below |
| `db` | Database targeting for non-PHP projects: `service` (e.g. `mysql`, `postgres`) and `database` name |

### Basic example

```yaml
php_version: "8.4"
node_version: "22"
framework: laravel
secured: true
services:
  - mysql
  - redis
```

### Custom container example

For non-PHP sites (Node.js, Python, Go, etc.), define a `container` section instead of `php_version` and `framework`:

```yaml
domains:
  - nestapp
container:
  port: 3000
  containerfile: Containerfile.lerd
services:
  - mysql
  - redis
custom_workers:
  dev-server:
    label: Dev Server
    command: npm run start:dev
    restart: always
```

When `container` is present, `php_version`, `framework`, and `node_version` are ignored.

#### `container` fields

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `port` | yes | | Port the app listens on inside the container |
| `containerfile` | no | `Containerfile.lerd` | Path to the Containerfile (relative to project root) |
| `build_context` | no | `.` | Build context directory (relative to project root) |

See [Custom Containers](../features/custom-containers.md) for the full guide.

### Custom workers

Custom workers can be defined for any site type (PHP or custom container). Each entry in `custom_workers` maps a name to a worker config:

```yaml
custom_workers:
  queue:
    label: Queue Worker
    command: node dist/queue.js
    restart: always
  cron:
    label: Cron Job
    command: node dist/cron.js
    restart: on-failure
    schedule: minutely
```

#### Worker config fields

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `label` | no | worker name | Display name in the dashboard |
| `command` | yes | | Shell command to run inside the container |
| `restart` | no | `always` | `always` or `on-failure` |
| `schedule` | no | | systemd OnCalendar expression for timer-based workers (e.g. `minutely`, `*-*-* *:00:00`) |
| `conflicts_with` | no | | List of worker names to stop before starting this one |

Worker definitions stay in `custom_workers` permanently. The `workers` field (a separate list of names) tracks which are currently active and is synced automatically by start/stop commands.

### Inline custom service definitions

Custom services can be defined directly in `.lerd.yaml` instead of (or in addition to) registering them with `lerd service add`. This makes the project fully self-contained — cloning it and running `lerd link` is enough to reproduce the environment.

```yaml
php_version: "8.4"
node_version: "22"
framework: laravel
secured: true
services:
  - redis
  - mongodb:
      image: docker.io/library/mongo:7
      ports:
        - 27017:27017
      environment:
        MONGO_INITDB_ROOT_USERNAME: root
        MONGO_INITDB_ROOT_PASSWORD: secret
      data_dir: /data/db
      description: "MongoDB document store"
      env_vars:
        - MONGO_URI=mongodb://root:secret@lerd-mongodb:27017/{{site}}
      site_init:
        exec: >
          mongosh admin -u root -p secret --eval
          "db.getSiblingDB('{{site}}').createCollection('_init')"
```

The inline definition schema is identical to a [custom service YAML file](../usage/services.md#yaml-schema). On apply, the service is registered to `~/.config/lerd/services/<name>.yaml` then started.

If a service with that name already exists locally and the definitions differ, a diff is shown and you are asked whether to replace it:

```
~ service/mongodb already exists and differs:

--- service/mongodb (current)
+++ service/mongodb (.lerd.yaml)
@@ -1,4 +1,4 @@
 image: docker.io/library/mongo:7
-description: MongoDB
+description: MongoDB document store
 ...

Replace service/mongodb with the version from .lerd.yaml? (y/N)
```

### Custom frameworks

When `lerd init` runs in a project that uses a custom framework (one added with `lerd framework add`), the full framework definition is embedded under `framework_def`. On a fresh machine the definition is restored automatically before linking — no manual `lerd framework add` step needed.

```yaml
framework: wordpress
framework_def:
  label: WordPress
  public_dir: .
  detect:
    - file: wp-config.php
  env:
    file: .env
  ...
```

If a framework with that name already exists locally and differs from the embedded definition, a diff is shown before applying.

### Applying `.lerd.yaml`

The config is applied whenever `lerd link` or `lerd init` runs in the project root:

- **`lerd link`** — framework definition restored, `.node-version` written, PHP version applied, HTTPS toggled, services registered and started.
- **`lerd init`** — installs PHP FPM if needed, then runs `lerd link` (which applies everything above). Re-runs the wizard if `--fresh` is passed.

Commit `.lerd.yaml` to the repository. On a fresh machine, `lerd link` is sufficient to reproduce the full local environment.

The Lerd watcher also monitors `.lerd.yaml` for changes. When you switch branches with a different config the PHP and Node versions are re-detected and applied automatically — no manual `lerd link` or `lerd init` needed. See [Automatic version switching](../features/project-setup.md#automatic-version-switching) for details.

`lerd isolate`, the UI PHP version selector, and the MCP `site_php` tool all keep `php_version` in sync when this file exists.

`lerd secure`, `lerd unsecure`, the UI HTTPS toggle, and the MCP `secure`/`unsecure` tools keep `secured` in sync when this file exists.
