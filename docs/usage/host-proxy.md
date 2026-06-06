---
title: Host-Proxy Sites
description: Run any dev server on the host with nginx reverse-proxying the domain to it
---

# Host-Proxy Sites

A host-proxy site runs your project's dev server directly on the host instead of inside a container, and nginx reverse-proxies the site's domain to it. The dev server can be anything that listens on a port: a Node tool (NestJS, Next, Nuxt, Vite, Angular), a Python server (Django, FastAPI/uvicorn), Rails, a Go or Rust binary, or any command you'd normally run by hand. Lerd supervises it as a worker so it auto-restarts, has logs, and self-heals.

Compared to [custom containers](custom-containers.md), host-proxy sites skip the image build and the container entirely, so they start instantly and file-watch/HMR works natively (no virtiofs inotify caveat). The trade-off is that the dev server runs on the host, so it reaches lerd's services over loopback rather than the container network. Lerd's env writer handles that for you (see [Env](#env)).

## Quick start

For a Node project, `lerd init` sets it up for you. When a `package.json` is present and there's no PHP, the wizard offers proxy mode as the default, picks the dev script (`start:dev`, `dev`, `serve`, `start` in preference order), auto-assigns a free port, and asks about HTTPS and services:

```bash
cd ~/Projects/api.example.com
lerd init
lerd link
```

For any other language, write the `proxy` block in `.lerd.yaml` by hand and link. The command and port are all lerd needs:

```yaml
domains:
  - api.example.com
secured: true
services:
  - postgres
  - redis
proxy:
  command: python manage.py runserver 0.0.0.0:8000
  port: 8000
```

```bash
lerd link
```

Lerd starts the dev server, generates the proxy vhost, and serves the domain. Open `https://api.example.com.test` (or whatever TLD you configured).

## Configuration

The `proxy` section in `.lerd.yaml` is mutually exclusive with `container:` (a site is one or the other, and `lerd check` rejects setting both).

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `command` | no | | Dev command lerd supervises, e.g. `npm run start:dev`, `python manage.py runserver`, `bin/rails server`. Empty means proxy-only: you run the server yourself and lerd only wires the proxy. |
| `port` | yes | | Port the dev server listens on. lerd injects this and proxies to it. |
| `ssl` | no | `false` | Set to `true` if the dev server serves HTTPS on its port (nginx proxies via `https://` with `proxy_ssl_verify off`). |
| `port_env_key` | no | `PORT` | Environment variable the port is injected as. Most servers read `PORT`; set this if yours uses a different name. |

## Ports

For Node projects the port is auto-assigned: `lerd init` starts from a default and walks up to the first free port, skipping any port another host-proxy site reserves and any port already bound on the host (so it never silently collides with a lerd service, for example gotenberg on 3000). A port named explicitly in the command is respected and reused. For hand-written configs, set `port` to whatever your server listens on.

Lerd sets both sides of the contract: it injects the chosen port via `PORT` (or `port_env_key`) so a server that honours it binds there, and points nginx at the same port. It does not probe the live socket, so a server that ignores the env var and hardcodes a different port will not be reached. For those, name the port directly in the command (for example `vite --port 5173 --strictPort`, or `runserver 0.0.0.0:8000`) so both sides agree.

## Lifecycle

The dev server is the site's main process, not a togglable worker. Its health drives the site's running indicator, and its lifecycle follows the site:

- `lerd link` and `lerd unpause` start it.
- `lerd pause` stops it (and replaces the vhost with a landing page).
- `lerd restart` restarts it.
- `lerd unlink` stops it and removes its unit.

If it dies it is restarted automatically (`Restart=always`), and lerd's worker-heal pass recovers it if it ever gets stuck, while leaving paused sites alone. Because pause already stops it, there is deliberately no separate start/stop toggle for the dev server in the dashboard.

The dashboard shows a **Proxy** badge with the port (mirroring the container badge custom-container sites get), and the dev server's journal is available under a read-only **Dev Server** logs tab. From the CLI:

```bash
journalctl --user -u lerd-app-<site> -f
```

## Env

A host-proxy app runs on the host, off the podman bridge, so it can't resolve container DNS names like `lerd-postgres`. `lerd env` rewrites service connections accordingly: any `lerd-<name>` host, whether bare (`DB_HOST=lerd-postgres`) or embedded in a URL (`MONGO_DSN=...@lerd-mongo:27017/db`), becomes `127.0.0.1`, and the port is remapped from the container port to the service's published host port (so mariadb's `3306` becomes `3411` while redis stays `6379`).

```bash
lerd env
```

Run this after linking so the app can reach postgres, redis, and friends on loopback. `lerd env` is geared to the common `.env` conventions; if your stack reads connection settings from somewhere else, point it at `127.0.0.1` and the service's published host port yourself.

## Services

Add services in `.lerd.yaml` exactly as for any other site. They run as lerd-managed containers and publish on the host's loopback interface, which is where `lerd env` points the app.

```yaml
proxy:
  command: npm run start:dev
  port: 3000
services:
  - postgres
  - redis
```

## HTTPS

`lerd secure` and `lerd unsecure` work with host-proxy sites. nginx terminates TLS and proxies plain HTTP to the dev server.

If the dev server serves HTTPS on its own port, set `ssl: true` under `proxy:` so nginx proxies via `https://` and skips certificate verification.

## Proxy-only mode

Leave `command` empty (or unset) to run the server yourself. Lerd only wires the nginx vhost to the configured port and supervises nothing:

```yaml
proxy:
  port: 3000
```

Start your server however you like; nginx proxies the domain to it.

## Worktrees

[Git worktrees](../features/git-worktrees.md) of a host-proxy site each run their own dev server from the worktree checkout, mirroring the parent's command on their own auto-assigned port, behind the worktree domain (`branch.site.test`) using the parent's wildcard certificate for HTTPS. Add one from the dashboard's worktree modal or with `git worktree add`; lerd detects it and wires the per-worktree dev server automatically. The modal omits the assets build step for host-proxy sites since the dev server runs continuously.

[Database isolation and cloning](database.md) work for host-proxy worktrees: lerd clones the parent database into a per-worktree database and repoints the worktree's `.env`. Migrations are never run as part of isolation, the clone already carries the schema, so there is no need for lerd to know your migration command. If you isolate with an empty schema instead of cloning, run your own migration command in the worktree.

## CLI commands

| Command | Description |
|---------|-------------|
| `lerd init` | Detect a Node project and run the host-proxy wizard |
| `lerd link` | Start the dev server and generate the proxy vhost |
| `lerd unlink` | Stop the dev server, remove its unit and vhost |
| `lerd env` | Wire service connections to loopback for the host app |
| `lerd secure` / `lerd unsecure` | Toggle HTTPS |
| `lerd pause` / `lerd unpause` | Pause/resume the site and dev server |
| `lerd restart` | Restart the dev server |
