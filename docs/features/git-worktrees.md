# Git Worktrees

Lerd treats every [git worktree](https://git-scm.com/docs/git-worktree) as a first-class scope: each branch checkout gets its own subdomain, its own optional database, its own PHP and Node versions, and its own slot in the dashboard. Branch names are sanitised to be subdomain-safe — `/`, `_`, and `.` are replaced with `-`, and non-alphanumeric characters are stripped.

```bash
cd ~/Lerd/myapp                                    # parent site, branch: main
git worktree add ../myapp-feature feature/auth     # creates the worktree
# Lerd writes the vhost feature-auth.myapp.test → ~/Lerd/myapp-feature/public
```

Plain `git worktree add` from any tool (CLI, IDE, GitLens) is enough to get a usable URL. For the full opinionated flow with prompts for DB isolation and a frontend build, use the `lerd worktree add` wrapper described below.

---

## `lerd worktree add` and `lerd worktree remove`

The wrapper commands mirror `git worktree`'s subcommand layout — every flag passes straight through to git — and add an interactive setup pipeline on top.

### `lerd worktree add <git args>`

```bash
cd ~/Lerd/myapp
lerd worktree add feature/auth                     # check out an existing branch
lerd worktree add -b feat-x                        # create a new branch named feat-x
lerd worktree add --detach feat-x abc1234          # detached checkout at a commit
lerd worktree add --track -b feat-x origin/feat    # tracking branch
```

After git completes, the wrapper:

1. Polls until the watcher has installed dependencies (`composer install` + `npm ci`) and synced the worktree's `.env`. The `.env` is set up *before* installs so `npm` build steps that read `VITE_*` env vars compile against the right values.
2. Prompts which production-build script to run (any of `build`, `prod`, `build:prod`, `build-prod`, `production` declared in `package.json` is offered). The default is **Skip — I'll run npm run dev (or build) myself**, since active development usually wants `npm run dev`'s hot reload.
3. Prompts how to set up the worktree's database — see [Per-worktree database](#per-worktree-database) below.
4. If you pick "isolated empty", asks whether to run `php artisan migrate --force` against the new schema right away.

Skipping the build step leaves the worktree without a Vite manifest, which means the first request will throw `ViteManifestNotFoundException` until you run `npm run dev` or `npm run build` yourself. That's intentional — the alternative is silently rendering main's compiled UI on the worktree, which is worse.

### `lerd worktree remove <git args>`

```bash
lerd worktree remove main
lerd worktree remove --force main          # discards local modifications
```

The wrapper runs `git worktree remove` and, when needed, falls back interactively. If git refuses with the *use --force* hint (modified or untracked files in the worktree) the wrapper offers a select prompt to retry with `--force` instead of forcing the user to rerun the command.

After git succeeds the wrapper asks whether to delete the worktree's isolated database. The default is **Keep the database** — if you re-create the worktree on the same branch later, the dashboard prompt will offer to reuse the preserved data without rebuilding it. Picking **Drop** drops the database and removes its registry entry.

---

## Auto-setup pipeline (any `git worktree add`)

Whether you use `lerd worktree add` or the bare `git` command, the daemon's watcher (`lerd-watcher`) sees the new entry under `.git/worktrees/` and runs the same setup steps in this order:

1. Wait for `HEAD` to be a final ref or SHA — git writes `gitdir`/`HEAD` over multiple steps and the watcher must avoid acting on a half-written detached state.
2. Seed `vendor/` and `node_modules/` from the main repo when the worktree's `composer.lock` / JS lockfile matches main's, using reflinks where the filesystem supports them (btrfs, xfs-reflink, APFS) and a plain copy elsewhere.
3. Sync `.env` from main with `APP_URL` rewritten to the worktree's vhost domain. When `.lerd.yaml` defines `env_overrides`, those templates are resolved instead (see [env overrides](#env-overrides) below).
4. Run `composer install` (skipped when the marker is at-or-newer than `composer.lock`) and `npm ci` / `pnpm install --frozen-lockfile` / `yarn install --immutable` / `bun install --frozen-lockfile` (skipped under the same marker rule).
5. Generate the worktree's nginx vhost.

Frontend build (`npm run build`) is **not** part of the watcher pipeline — it's heavy, project-specific, and can fail silently. `lerd worktree add` runs it interactively after asking; using bare `git worktree add` you run it yourself.

`public/build/` is also intentionally not seeded from main: it's a build artefact of the source tree, and copying it would render main's compiled UI on the worktree until the user noticed.

| Resource | Behaviour |
|---|---|
| `vendor/` | Reflink/copy from main when `composer.lock` matches; otherwise skip and let `composer install` build from scratch (no stale autoload entries). |
| `node_modules/` | Same lockfile-match guard against `pnpm-lock.yaml` / `yarn.lock` / `bun.lock*` / `package-lock.json` / `npm-shrinkwrap.json` (whichever exists). |
| `public/build/` | Not seeded. Run `npm run dev` (Vite dev server, hot reload) or `npm run build` (static manifest) inside the worktree. |
| `.env` | Copied from main; `APP_URL` rewritten to `http(s)://<branch>.<site>.test` (or resolved via `env_overrides` when defined). Realigned on every subsequent watcher pass so a branch rename keeps the value current. |

::: info Why not symlink?
Earlier lerd versions symlinked `vendor/` to save disk. PHP resolves `__DIR__` through symlinks to the real path, so Composer's `ClassLoader` would initialise against the main repo and silently load stale classes. Real copies (or reflinks) avoid the problem at no meaningful disk cost on modern filesystems.
:::

---

## HTTPS

If the parent site is secured with `lerd secure`, worktree subdomains inherit HTTPS automatically. When a worktree is created on a secured site, lerd reissues the parent's mkcert certificate to include `*.branch.myapp.test` SANs, so deep subdomains (e.g. `app.feature-auth.myapp.test` for multi-tenant apps) are also covered. The worktree's nginx vhost includes `*.branch.myapp.test` in its `server_name` directive.

```bash
lerd secure myapp
# myapp.test                          → https
# feature-auth.myapp.test             → https  (automatic)
# app.feature-auth.myapp.test         → https  (wildcard SAN)
```

`APP_URL` in each worktree's `.env` is rewritten to `https://` when you secure the parent (and back to `http://` on `lerd unsecure`).

## Env overrides

By default lerd only rewrites `APP_URL` in worktree `.env` files. Multi-tenant apps and other projects that derive multiple env variables from the site domain can define `env_overrides` in `.lerd.yaml`:

```yaml
env_overrides:
    APP_URL: "{{scheme}}://app.{{domain}}"
    CENTRAL_DOMAIN: "{{domain}}"
    CACHE_DRIVER: redis
```

Values can use template placeholders or be plain static strings. When a worktree is created (or the watcher re-syncs), each value is resolved and written into the worktree's `.env`.

| Placeholder | Resolves to | Example |
|-------------|-------------|---------|
| `{{domain}}` | Worktree domain | `feature-branch.myapp.test` |
| `{{scheme}}` | `http` or `https` | `https` |
| `{{site}}` | Database-safe name (underscored) | `feature_branch_myapp_test` |

When `APP_URL` is present in `env_overrides` it takes precedence over the default `scheme://domain` rewrite. Without `env_overrides`, behaviour is unchanged.

---

## Web UI

In the Sites tab, the site detail panel's path line carries an inline branch picker (`/path/to/project · git:(main) 3 ▾`) instead of stacking a row per worktree. The chevron opens a dropdown listing main + every active worktree, each with its derived domain and an open-in-browser shortcut.

Picking a branch re-scopes the rest of the detail view to that worktree:

- The site title and the **Open** / **Terminal** buttons target the worktree's domain and checkout path.
- The **App logs** tab tails `storage/logs` from the worktree's directory rather than main's.
- The **Tinker** tab REPL runs inside the worktree's PHP context (its own `.env`, its own vendor).
- The **PHP** and **Node** version selectors show the worktree's effective version. A dashed violet border indicates "Inherits from main"; changing the value persists a worktree-only override.
- Worker toggles (queue, schedule, Horizon, Reverb, custom workers) collapse into a "Workers run from main" pill — those run against main's checkout regardless of which worktree is active. Switch to main to start or stop them.
- The domain-edit pencil disappears (worktree domains are derived from the parent's primary).

### Per-worktree PHP and Node versions

Each worktree can pin its own PHP or Node version without affecting the parent site — the natural way to test a runtime upgrade on a feature branch.

When you pick a non-default value from the version selector while a worktree is active, lerd writes the override to `.lerd.yaml` *inside that worktree's checkout*. The file lives in the working tree, so the choice travels with the branch in git.

```yaml
# .lerd.yaml inside the feat-php84 worktree only
php_version: "8.4"
node_version: "24"
```

The override is honoured wherever lerd materialises worktree state on disk: vhost generation on add, rename, pause/unpause, and `lerd secure`/`lerd unsecure`. Worktrees with no override inherit the parent's pinned version (not the highest-installed satisfier of `composer.json`/`package.json` constraints — that detection only kicks in for unregistered directories).

Site-level resources stay shared and cannot be overridden per worktree: domain (derived from the parent), TLS certificate (parent's wildcard cert), LAN share port (worktree-scoped LAN share is a separate toggle), workers, and any custom container settings.

### Per-worktree database

By default every worktree shares the parent site's database. The dashboard's **Isolated DB** toggle and the `lerd worktree add` prompt opt the worktree into its own schema, named `<parent_db>_<sanitized_branch>` in the same service the parent uses (mysql, mariadb, or postgres). The worktree's `.env` is rewritten so `DB_DATABASE` points at the new schema, and `db_isolated: true` is persisted to the worktree's `.lerd.yaml` so the choice travels with the branch.

When isolation is enabled lerd asks where the new schema should start from:

| Source | What happens |
|---|---|
| Empty | New schema with no tables. The wrapper then offers to run `php artisan migrate --force` against it. |
| Clone from main | `mysqldump --single-transaction` (or `pg_dump`) of the parent's DB piped into the new schema, entirely inside the service container. |
| Clone from another isolated worktree | Same dump/restore pipeline, source is an existing isolated worktree's DB. Useful when staging migrations on top of an already-migrated branch. |

The isolated database survives `lerd worktree remove` by default — the wrapper asks at the end whether to drop it, and **Keep the database** is the default. Re-adding the same branch via `lerd worktree add` later detects the preserved entry and offers two extra options at the top of the DB-isolation prompt:

- **Reuse preserved isolated DB** — reconnects the worktree to the same data with no schema changes.
- **Reset preserved DB to a fresh empty schema** — drops the existing DB, recreates it empty, then offers to run migrations.

The same toggle is available on the dashboard for already-active worktrees: flipping **Isolated DB** off drops the database and restores the parent's `DB_DATABASE` value in `.env`. The toggle only appears when the parent uses a lerd-managed mysql/mariadb/postgres service. Sqlite is naturally file-based and isolated per checkout, no opt-in needed.

### Per-worktree LAN share

LAN share has a separate toggle that's worktree-aware: when a worktree is active in the dashboard, the toggle controls a proxy bound to a per-worktree port (next free `>= 9100` across all sites and worktrees). The proxy targets the worktree's vhost domain so devices on your network reach the worktree's URL directly — no DNS setup on the client. Removing the worktree releases the port via the watcher's cleanup pass; the dashboard QR-code popover honours `?branch=` so the QR encodes the worktree's URL.

---

## Cleanup ordering

When a worktree is removed (via `git worktree remove` directly or `lerd worktree remove`) the watcher tears state down in this order so that any earlier failure leaves the database intact:

1. nginx vhost (URL stops resolving)
2. LAN-share proxy + registry entry (port released)
3. Isolated database — *only* via `lerd worktree remove`'s explicit prompt or the daemon's `scanWorktrees` startup sweep. Plain `git worktree remove` leaves the DB and its registry entry alone, so the user can recover by re-adding the worktree without losing migrations or seed data.

The startup sweep also catches any registry entries whose worktree directory disappeared while the watcher was offline — restarting `lerd-watcher` reconciles state.

---

## `lerd sites` output

Worktrees are shown indented under their parent site, with their own effective PHP / Node / framework version (which may differ from the parent's when an override is set in `.lerd.yaml`):

```
NAME            DOMAIN                   PHP    NODE   TLS   PATH
myapp           myapp.test               8.5    22     ✓     ~/Lerd/myapp
↳ feature-auth  feature-auth.myapp.test  8.5    -      -     ~/Lerd/myapp-feature
↳ feat-php84    feat-php84.myapp.test    8.4    24     -     ~/Lerd/myapp-php84
```
