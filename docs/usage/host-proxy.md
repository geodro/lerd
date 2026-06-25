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
| `host_env_key` | no | `HOST` | Environment variable the bind address (`0.0.0.0`) is injected as. Most servers read `HOST`; set this if yours uses a different name, e.g. `HOSTNAME` for a Next.js standalone server. |
| `inject_host` | no | `true` | Whether lerd injects the bind-address env (see [Binding](#binding)). Set `false` to opt out entirely, for a server that reads `HOST` for something else or manages its own bind. The port injection is unaffected. |

## Ports

For Node projects the port is auto-assigned: `lerd init` starts from a default and walks up to the first free port, skipping any port another host-proxy site reserves and any port already bound on the host (so it never silently collides with a lerd service, for example gotenberg on 3000). A port named explicitly in the command is respected and reused. For hand-written configs, set `port` to whatever your server listens on.

Lerd sets both sides of the contract: it injects the chosen port via `PORT` (or `port_env_key`) so a server that honours it binds there, and points nginx at the same port. It does not probe the live socket, so a server that ignores the env var and hardcodes a different port will not be reached. For those, name the port directly in the command (for example `vite --port 5173 --strictPort`, or `runserver 0.0.0.0:8000`) so both sides agree.

## Binding

nginx runs inside a container and reaches your dev server over the host gateway, not loopback. A server bound to `127.0.0.1` (the common dev default) is therefore unreachable through the proxy: depending on the framework it surfaces as a connection error, or as a stray all-interfaces HMR socket answering every request with `426 Upgrade Required` (the symptom seen with Nuxt).

To avoid this, lerd injects `HOST=0.0.0.0` (or `host_env_key`) alongside the port, which Nuxt, NestJS, and most Node servers honour to bind all interfaces. A `HOST` you set yourself later in the command still wins. Servers that ignore the env var (notably the raw Vite CLI, which reads a flag) must bind explicitly: name `--host` in the command, for example `vite --host --port 5173 --strictPort`.

If a server reads `HOST` for something else, or you already set it in your `.env` (dotenv won't override a variable already in the environment, so the injected `0.0.0.0` would win), set `inject_host: false` to suppress the injection and manage binding yourself. The port injection is unaffected.

## Vite

Vite checks the request's `Host` header against its `allowedHosts` list and answers anything else with `403 Blocked request`. Because nginx proxies with the site domain as the `Host`, a default Vite project returns 403 through the lerd proxy until you allow the domain. Add it to `server.allowedHosts` in `vite.config` (or set `allowedHosts: true`). The raw Vite CLI also ignores `HOST` (see [Binding](#binding)), so add `--host` to the command. `lerd init` prints these reminders when it detects a Vite dev command.

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

## Trust and consent

A host-proxy dev server runs your project's command directly on the host (as a systemd `--user` unit on Linux, launchd on macOS), outside any container. Because the command can come from a project's committed `.lerd.yaml`, lerd will not silently supervise a command you have not agreed to run. Three gates enforce this:

- **Explicit setup only.** A dev-server unit is only ever created by an explicit action: `lerd init`, `lerd link`, or clicking Link in the dashboard. Nothing is installed just because a folder appears in a parked directory.
- **Command confirmation.** When you `lerd link` a project whose `.lerd.yaml` already carries a `proxy:` block you did not author through the wizard, lerd prints the exact command and asks before installing and starting it. Re-linking with the same approved command, choosing it in the `lerd init` wizard, clicking Link in the dashboard, or passing `lerd link --yes` all count as consent and skip the prompt. A non-interactive `lerd link` with an unapproved command is refused rather than run blindly.
- **Drift protection.** lerd records the approved command in its site registry. If `.lerd.yaml`'s dev command later changes (for example after a `git pull`), `lerd start` and boot restore will not auto-run the new command; they warn and wait for you to `lerd link` again to review and approve it.

Two global settings in `config.yaml` tune this (both default to the safe value, so existing installs are unaffected):

```yaml
host_proxy:
  disabled: false           # set true to refuse all host-proxy dev servers
  skip_confirmation: false  # set true to link without the confirmation prompt
```

Note that this gate is about consent, not sandboxing: a dev server you approve runs with your full user privileges, exactly as if you had run it in a terminal. Only link projects you trust, the same way you would before running `npm run dev` or `npm install` on them.

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
