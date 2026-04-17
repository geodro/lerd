# Containers walkthrough

End-to-end: from an empty Node, Python, or Go project to an HTTPS site running at `https://myapp.test` with services, workers, and automatic rebuilds on Containerfile changes.

::: info Prerequisites
You've already run `lerd install` once on this machine. If not, see [Installation](installation.md).
:::

::: info When to use this
Use a custom container when your project isn't PHP, or when a PHP project needs a non-standard runtime (alternate PHP build, FrankenPHP, RoadRunner). PHP projects that fit the built-in PHP-FPM image should use the [Laravel](laravel.md), [Symfony](symfony.md), or [WordPress](wordpress.md) walkthroughs instead.
:::

---

## 1. Add a `Containerfile.lerd`

Drop a `Containerfile.lerd` at the project root. Lerd bind-mounts the project directory into the container at the same absolute path at runtime, so you don't need `WORKDIR` or `COPY`. Only install tooling (global CLIs, system packages, language runtimes).

::: code-group

```dockerfile [Node.js (NestJS, Next.js, Express)]
FROM node:20-alpine
RUN apk add --no-cache git
RUN npm install -g nodemon pnpm
CMD ["npm", "run", "start:dev"]
```

```dockerfile [Python (FastAPI, Django)]
FROM python:3.12-slim
RUN apt-get update && apt-get install -y --no-install-recommends build-essential \
    && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir uvicorn[standard] watchfiles
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000", "--reload"]
```

```dockerfile [Go]
FROM golang:1.23-alpine
RUN go install github.com/air-verse/air@latest
CMD ["air"]
```

```dockerfile [Ruby on Rails]
FROM ruby:3.3-slim
RUN apt-get update && apt-get install -y --no-install-recommends build-essential libpq-dev nodejs \
    && rm -rf /var/lib/apt/lists/*
CMD ["bin/rails", "server", "-b", "0.0.0.0"]
```

:::

::: tip Why bind mount, not COPY?
Lerd is a dev environment: source lives on your host, edits are live. Baking source into the image would force a rebuild on every save. Your production Dockerfile still uses `COPY` and multi-stage builds, just name it something other than `Containerfile.lerd`.
:::

---

## 2. Run `lerd init`

```bash
cd ~/projects/myapp
lerd init
```

When no PHP project is detected and a `Containerfile.lerd` exists, the wizard switches to custom container mode:

```
? Container port: 3000
? Containerfile: Containerfile.lerd
? Enable HTTPS? Yes
? Services: [mysql, redis]
Saved .lerd.yaml
```

The wizard writes `.lerd.yaml`:

```yaml
domains:
  - myapp
container:
  port: 3000
secured: true
services:
  - mysql
  - redis
```

::: info Port matters
`container.port` is the port your app listens on *inside* the container. Nginx will `proxy_pass` to that port on the container's internal network address. You don't publish it on the host.
:::

---

## 3. Link the site

```bash
lerd link
```

`lerd link`:

1. Builds the image, tagged `lerd-custom-myapp:local` (Containerfile hash is cached, so unchanged files skip rebuild)
2. Writes a systemd quadlet so the container starts on boot
3. Joins the container to the shared `lerd` network
4. Generates an nginx vhost that reverse-proxies `myapp.test` to the container
5. Reloads nginx

::: warning Order matters
`lerd link` must run **after** both `Containerfile.lerd` and `.lerd.yaml` exist. If you ran `lerd link` before writing `.lerd.yaml`, Lerd registered the project as a PHP site. Run `lerd unlink`, then `lerd init`, then `lerd link` again.
:::

---

## 4. Reach your services

Services on the `lerd` network are reachable by hostname. Wire them into your app's env file:

::: code-group

```bash [Node (.env)]
DATABASE_URL=mysql://root:lerd@lerd-mysql:3306/myapp
REDIS_URL=redis://lerd-redis:6379
MAIL_HOST=lerd-mailpit
MAIL_PORT=1025
```

```bash [Python (.env)]
DATABASE_URL=postgresql://postgres:lerd@lerd-postgres:5432/myapp
REDIS_URL=redis://lerd-redis:6379/0
```

```bash [Go (.env)]
DATABASE_DSN=postgres://postgres:lerd@lerd-postgres:5432/myapp?sslmode=disable
REDIS_ADDR=lerd-redis:6379
```

:::

| Service | Host | Default port | Default password |
|---|---|---|---|
| MySQL | `lerd-mysql` | `3306` | `lerd` (user `root`) |
| PostgreSQL | `lerd-postgres` | `5432` | `lerd` (user `postgres`) |
| Redis | `lerd-redis` | `6379` | (none) |
| Meilisearch | `lerd-meilisearch` | `7700` | (none) |
| RustFS (S3) | `lerd-rustfs` | `9000` | `lerd` / `lerdpassword` |
| Mailpit (SMTP) | `lerd-mailpit` | `1025` | (none) |

See [Services](../usage/services.md) for the full credential matrix, including host-tool ports (127.0.0.1) versus container-network hostnames.

Create the database:

```bash
lerd db:create myapp
```

See [Database](../usage/database.md) for imports, shells, and switching engines.

---

## 5. Add workers

Long-running processes (dev server, queue consumer, scheduler) live under `custom_workers` in `.lerd.yaml`. Each worker runs via `podman exec` inside the same container as your app.

```yaml
container:
  port: 3000
custom_workers:
  dev:
    label: Dev Server
    command: npm run start:dev
    restart: always
  queue:
    label: Queue Worker
    command: node dist/jobs/worker.js
    restart: on-failure
  cron:
    label: Nightly Cleanup
    command: node dist/jobs/cleanup.js
    schedule: daily
```

Start and stop them like any other worker:

```bash
lerd worker list
lerd worker start dev
lerd worker start queue
lerd worker stop queue
```

Workers appear in the [Web UI](../features/web-ui.md) with live logs. For `schedule:` timers see [Queue Workers](../usage/queue-workers.md).

---

## 6. Hot reload (polling)

The project directory is bind-mounted, but **inotify events don't cross the Podman Machine boundary on macOS, and can be unreliable on Linux with virtiofs**. File watchers that rely on inotify need polling:

| Tool | Config |
|---|---|
| nodemon | `nodemon --legacy-watch src/main.js` |
| Vite | `server.watch.usePolling: true` in `vite.config` |
| Next.js | `WATCHPACK_POLLING=true` env var |
| NestJS | `nodemon.json`: `{"legacyWatch": true}` |
| webpack | `watchOptions: { poll: 1000 }` |
| uvicorn | `--reload --reload-delay 0.5` (uses `watchfiles`, already polls) |
| Django | `runserver` polls by default |
| air (Go) | polls by default |
| Rails | `config.file_watcher = ActiveSupport::FileUpdateChecker` + `rerun` gem |

Poll interval around 1 second is usually fine for development.

---

## 7. HTTPS

```bash
lerd secure
```

`lerd secure` issues an mkcert certificate for `myapp.test`, flips the nginx vhost to TLS, and regenerates the proxy config. Your app keeps receiving plain HTTP from nginx, which handles TLS termination.

If your app serves its own HTTPS (FrankenPHP with built-in TLS, a Go service with Let's Encrypt test certs), add `ssl: true` so nginx proxies via HTTPS with verification disabled:

```yaml
container:
  port: 3000
  ssl: true
```

See [HTTPS / TLS](../features/https.md) for wildcard certs and git worktree support.

---

## 8. Verify

```bash
lerd status
```

You should see `myapp` as `active`, the container as `running`, services healthy, and any started workers listed. Live logs for the container and workers live in the [Web UI](../features/web-ui.md) at `http://127.0.0.1:7073`.

Open the site:

```bash
lerd open
```

---

## Common stacks

::: code-group

```yaml [NestJS]
domains: [myapp]
container:
  port: 3000
secured: true
services:
  - mysql
  - redis
custom_workers:
  dev:
    label: Nest Dev
    command: npm run start:dev
    restart: always
  queue:
    label: BullMQ Worker
    command: node dist/queue/worker.js
    restart: on-failure
```

```yaml [Next.js]
domains: [shop]
container:
  port: 3000
secured: true
services:
  - postgres
  - redis
custom_workers:
  dev:
    label: Next Dev
    command: npm run dev
    restart: always
```

```yaml [FastAPI]
domains: [api]
container:
  port: 8000
secured: true
services:
  - postgres
  - redis
custom_workers:
  dev:
    label: Uvicorn
    command: uvicorn app.main:app --host 0.0.0.0 --port 8000 --reload
    restart: always
  celery:
    label: Celery Worker
    command: celery -A app.worker worker --loglevel=info
    restart: on-failure
```

```yaml [Go (air)]
domains: [api]
container:
  port: 8080
secured: true
services:
  - postgres
custom_workers:
  dev:
    label: Air
    command: air
    restart: always
```

```yaml [Rails]
domains: [shop]
container:
  port: 3000
secured: true
services:
  - postgres
  - redis
custom_workers:
  web:
    label: Puma
    command: bin/rails server -b 0.0.0.0
    restart: always
  sidekiq:
    label: Sidekiq
    command: bundle exec sidekiq
    restart: on-failure
```

:::

---

## What just happened

| Command | What it did |
|---|---|
| `lerd init` | Detected `Containerfile.lerd`, ran the container wizard, wrote `.lerd.yaml` with `container:`, services, and workers |
| `lerd link` | Built `lerd-custom-myapp:local`, wrote the quadlet, started the container on the `lerd` network, generated an nginx proxy vhost, reloaded nginx |
| `lerd db:create myapp` | Created the `myapp` database in the selected engine |
| `lerd secure` | Issued a mkcert cert, flipped the vhost to HTTPS |
| `lerd worker start dev` | Started `lerd-dev-myapp.service` which `podman exec`s into the container |

---

## Rebuilding after Containerfile changes

`lerd link` reuses the cached image (Containerfile MD5 hash). When you change `Containerfile.lerd`, rebuild explicitly:

```bash
lerd rebuild
```

This removes the old image, rebuilds from the current Containerfile, and restarts the container. No downtime for nginx or services.

`lerd restart` restarts the container *without* rebuilding, useful after changing a mounted config file that the app reads on startup.

---

## Next steps

- [Custom Containers reference](../usage/custom-containers.md): every `container:` field, worker option, and proxy quirk
- [Services walkthrough](services.md): add MongoDB, Elasticsearch, RabbitMQ, phpMyAdmin
- [Database](../usage/database.md): `lerd db:import`, `lerd db:shell`, switching engines
- [Queue Workers](../usage/queue-workers.md): `schedule:` timers, restart policies, health checks
- [HTTPS](../features/https.md): wildcard certs, git worktree subdomains
- [AI Integration (MCP)](../features/mcp.md): drive `lerd init`, `lerd link`, `lerd rebuild` from Claude Code or Cursor
