# Services walkthrough

Lerd ships with **MySQL, PostgreSQL, Redis, Meilisearch, RustFS, and Mailpit** built in. Anything else — MongoDB, phpMyAdmin, pgAdmin, Elasticsearch, MinIO, RabbitMQ — runs as a **custom service**: a YAML file dropped into `~/.config/lerd/services/`, registered with one command, and managed by `lerd service start/stop/list` exactly like the built-ins.

::: info Prerequisites
You've already run `lerd install` once on this machine. If not, see [Installation](installation.md).
:::

::: tip Drive it from your AI assistant
After running `lerd mcp:enable-global`, your AI assistant can call `service_add`, `service_start`, `service_stop`, and `service_remove` directly. See [AI Integration](../features/mcp.md).
:::

---

## How it works (30 seconds)

Three steps for any custom service:

```bash
# 1. Save the YAML somewhere
$EDITOR ~/.config/lerd/services/<name>.yaml

# 2. Register it with lerd
lerd service add ~/.config/lerd/services/<name>.yaml

# 3. Start it
lerd service start <name>
```

After step 2, the service appears in `lerd service list`, the Web UI Services panel, and `lerd start` / `lerd stop` cycles.

For the full YAML schema (env vars, `depends_on`, `site_init`, `{{site}}` placeholders, etc.) see [Services reference](../usage/services.md#yaml-schema).

---

## Recipe: MongoDB

```yaml
# ~/.config/lerd/services/mongodb.yaml
name: mongodb
image: docker.io/library/mongo:7
description: "MongoDB document store"
ports:
  - 27017:27017
environment:
  MONGO_INITDB_ROOT_USERNAME: root
  MONGO_INITDB_ROOT_PASSWORD: lerd
data_dir: /data/db
env_vars:
  - "MONGO_DATABASE={{site}}"
  - "MONGO_URI=mongodb://root:lerd@lerd-mongodb:27017/{{site}}?authSource=admin"
env_detect:
  key: MONGO_URI
  value_prefix: "mongodb://"
site_init:
  exec: >
    mongosh admin -u root -p lerd --eval
    "db.getSiblingDB('{{site}}').createCollection('_init');
     db.getSiblingDB('{{site_testing}}').createCollection('_init')"
```

```bash
lerd service add ~/.config/lerd/services/mongodb.yaml
lerd service start mongodb
```

| From | Host |
|---|---|
| Your app (PHP-FPM) | `lerd-mongodb:27017` |
| Host tools (Compass, mongosh) | `127.0.0.1:27017` |
| User / password | `root` / `lerd` |

When `lerd env` runs in a project that has `MONGO_URI=mongodb://...` in its `.env`, the connection string is rewritten to point at `lerd-mongodb` and a per-site database is created automatically.

---

## Recipe: phpMyAdmin

```yaml
# ~/.config/lerd/services/phpmyadmin.yaml
name: phpmyadmin
image: docker.io/phpmyadmin:latest
description: "phpMyAdmin web interface for MySQL"
ports:
  - 8080:80
environment:
  PMA_HOST: lerd-mysql
  PMA_USER: root
  PMA_PASSWORD: lerd
depends_on:
  - mysql
dashboard: http://localhost:8080
```

```bash
lerd service add ~/.config/lerd/services/phpmyadmin.yaml
lerd service start phpmyadmin
```

Open `http://localhost:8080`. Because of `depends_on: mysql`, `lerd service start phpmyadmin` boots MySQL first if it isn't already running, and `lerd service stop mysql` cascades down to phpMyAdmin.

---

## Recipe: pgAdmin

```yaml
# ~/.config/lerd/services/pgadmin.yaml
name: pgadmin
image: docker.io/dpage/pgadmin4:latest
description: "pgAdmin 4 web interface for PostgreSQL"
ports:
  - 8081:80
environment:
  PGADMIN_DEFAULT_EMAIL: admin@lerd.test
  PGADMIN_DEFAULT_PASSWORD: lerd
  PGADMIN_CONFIG_SERVER_MODE: "False"
data_dir: /var/lib/pgadmin
depends_on:
  - postgres
dashboard: http://localhost:8081
```

```bash
lerd service add ~/.config/lerd/services/pgadmin.yaml
lerd service start pgadmin
```

Open `http://localhost:8081`, log in with `admin@lerd.test` / `lerd`, then add a server with host `lerd-postgres`, port `5432`, user `postgres`, password `lerd`.

---

## Recipe: Adminer

A lightweight, single-file alternative to phpMyAdmin/pgAdmin that supports both MySQL and PostgreSQL:

```yaml
# ~/.config/lerd/services/adminer.yaml
name: adminer
image: docker.io/library/adminer:latest
description: "Universal database web client (MySQL + PostgreSQL + more)"
ports:
  - 8082:8080
depends_on:
  - mysql
dashboard: http://localhost:8082
```

```bash
lerd service add ~/.config/lerd/services/adminer.yaml
lerd service start adminer
```

Open `http://localhost:8082`. Choose the system (MySQL → host `lerd-mysql`, or PostgreSQL → host `lerd-postgres`), then user `root` / `postgres` and password `lerd`.

---

## Recipe: Elasticsearch

```yaml
# ~/.config/lerd/services/elasticsearch.yaml
name: elasticsearch
image: docker.io/elasticsearch:8.13.4
description: "Elasticsearch search engine"
ports:
  - 9200:9200
environment:
  discovery.type: single-node
  xpack.security.enabled: "false"
  ES_JAVA_OPTS: "-Xms512m -Xmx512m"
data_dir: /usr/share/elasticsearch/data
env_vars:
  - "ELASTICSEARCH_HOST=http://lerd-elasticsearch:9200"
env_detect:
  key: ELASTICSEARCH_HOST
```

```bash
lerd service add ~/.config/lerd/services/elasticsearch.yaml
lerd service start elasticsearch
```

| From | Host |
|---|---|
| Your app | `http://lerd-elasticsearch:9200` |
| Host tools | `http://127.0.0.1:9200` |

---

## Recipe: RabbitMQ

```yaml
# ~/.config/lerd/services/rabbitmq.yaml
name: rabbitmq
image: docker.io/library/rabbitmq:3-management
description: "RabbitMQ message broker with management UI"
ports:
  - 5672:5672
  - 15672:15672
environment:
  RABBITMQ_DEFAULT_USER: lerd
  RABBITMQ_DEFAULT_PASS: lerd
data_dir: /var/lib/rabbitmq
env_vars:
  - "RABBITMQ_HOST=lerd-rabbitmq"
  - "RABBITMQ_PORT=5672"
  - "RABBITMQ_USER=lerd"
  - "RABBITMQ_PASSWORD=lerd"
env_detect:
  key: RABBITMQ_HOST
dashboard: http://localhost:15672
```

```bash
lerd service add ~/.config/lerd/services/rabbitmq.yaml
lerd service start rabbitmq
```

Management UI at `http://localhost:15672` (`lerd` / `lerd`).

---

## Verify

```bash
lerd service list
```

Each registered custom service shows up with a `[custom]` marker. `[pinned]` means it stays running across `lerd start`/`lerd stop` cycles. Indented sub-lines show dependency or auto-stop reasons.

```bash
lerd service status mongodb     # systemd unit status
lerd service stop mongodb        # stop without removing
lerd service remove mongodb      # stop + remove quadlet + delete YAML
```

The data directory at `~/.local/share/lerd/data/<name>/` is **not** deleted by `service remove` — wipe it manually if you want a clean slate.

---

## Per-site auto-injection

Three of the recipes above (`mongodb`, `elasticsearch`, `rabbitmq`) include `env_detect` and `env_vars`. When you run `lerd env` in a project that already references one of those services in its `.env` (e.g. `MONGO_URI=` is set), lerd:

1. Starts the service if it isn't already running
2. Substitutes <code v-pre>{{site}}</code> in the env vars with the project's site handle
3. Writes the resulting variables into the project's `.env`
4. Runs `site_init.exec` inside the container (MongoDB recipe) to create per-site databases

This means dropping the YAML once is enough — every project that needs the service gets wired up automatically on `lerd env` (which `lerd init` and `lerd setup` both call).

---

## Next steps

- [Services reference](../usage/services.md) — full YAML schema, dependency rules, custom command flags, RustFS / Mailpit / Soketi / stripe-mock built-in details
- [Configuration](../reference/configuration.md) — embedding services directly in `.lerd.yaml` so they ship with the repo
- [AI Integration (MCP)](../features/mcp.md) — manage services from your AI assistant
