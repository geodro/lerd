# Site Management

## Commands

| Command | Description |
|---|---|
| `lerd init` | Interactive wizard — choose PHP version, HTTPS, and services, then save `.lerd.yaml` and apply |
| `lerd init --fresh` | Re-run the wizard with existing `.lerd.yaml` values as defaults |
| `lerd park [dir]` | Register all Laravel projects inside `dir` (defaults to cwd) |
| `lerd unpark [dir]` | Remove a parked directory and unlink all its sites |
| `lerd link [domain]` | Register the current directory as a site (domain name without TLD, defaults to directory name) |
| `lerd unlink` | Unlink the current directory site (removes all domains) |
| `lerd domain add <name>` | Add an additional domain to the current site |
| `lerd domain remove <name>` | Remove a domain from the current site |
| `lerd domain list` | List all domains for the current site |
| `lerd sites` | Table view of all registered sites |
| `lerd open [name]` | Open the site in the default browser |
| `lerd share [name]` | Expose the site publicly via ngrok or Expose (auto-detected) |
| `lerd secure [name]` | Issue a mkcert TLS cert and enable HTTPS — updates `APP_URL` in `.env` |
| `lerd unsecure [name]` | Remove TLS and switch back to HTTP — updates `APP_URL` in `.env` |
| `lerd pause [name]` | Pause a site: stop its workers and replace the vhost with a landing page |
| `lerd unpause [name]` | Resume a paused site: restore its vhost and restart previously running workers |
| `lerd env` | Configure `.env` for the current project with lerd service connection settings |

---

## Project initialisation

`lerd init` runs an interactive wizard, writes the answers to `.lerd.yaml` in the project root, and then applies the configuration — linking the site, enabling HTTPS if requested, picking a database, and starting any required services.

```bash
cd ~/Projects/my-app
lerd init
```

```
? PHP version: 8.4
? Node version (leave blank to skip):
? Enable HTTPS? No
? Database:
  > SQLite (no service)
    MySQL (lerd-mysql)
    PostgreSQL (lerd-postgres)
? Services:
  ◉ redis
  ◯ meilisearch
  ◯ rustfs
  ◯ mailpit
Saved .lerd.yaml
Linked: my-app -> my-app.test (PHP 8.4, Node 22, Framework: laravel)
```

Wizard defaults are populated intelligently on first run:

- **PHP version** — from the site registry if already linked, otherwise from `.php-version`, `composer.json`, or the global default
- **Enable HTTPS** — pre-checked if the site is already secured
- **Database** — pre-selected from any database already in `.lerd.yaml`, otherwise from `DB_CONNECTION` in `.env` (or `.env.example` for a fresh clone), falling back to SQLite (Laravel's default for new projects)
- **Services** — pre-checked based on what's detected in the project's `.env` file (only non-database services here — the database is its own step)

The Database step is a single choice rather than a multi-select, so picking MySQL automatically deselects SQLite and vice-versa. After the wizard completes, `lerd env` runs automatically to write your choices to `.env`:

- **MySQL / PostgreSQL** — `DB_CONNECTION` and the related `DB_HOST` / `DB_PORT` / `DB_DATABASE` / `DB_USERNAME` / `DB_PASSWORD` keys are rewritten to point at `lerd-mysql` / `lerd-postgres`, the service is started if it isn't already, and the project database (plus a `_testing` variant) is created.
- **SQLite** — `DB_CONNECTION=sqlite` and `DB_DATABASE=database/database.sqlite` are written to `.env`, and the `database/database.sqlite` file is created if it doesn't exist. No service is started.

The choice is authoritative: if `.env` already had `DB_CONNECTION=mysql` from a previous setup and you switch to SQLite (or vice versa) in the wizard, lerd skips the auto-detection of the old database and applies your new pick instead.

The same prompt also appears when you run `lerd env` directly on a project whose `.env` says SQLite and whose `.lerd.yaml` doesn't yet have a database picked — for example, after cloning a project that wasn't created with `lerd init`. The prompt is skipped automatically when stdin isn't a TTY (e.g. `lerd setup --all` in CI), and for frameworks with explicit env service rules (`fw.env.services` in the YAML — Symfony, WordPress, etc.) since those don't use Laravel's `DB_CONNECTION` convention.

Persistence is one-way: lerd reads the source of truth from `.lerd.yaml` and writes only to `.env`. `.env.example` is never modified — it's only used as a template when `.env` doesn't exist yet.

The resulting `.lerd.yaml` is intended to be committed to the repository. On a new machine or after a reinstall, running `lerd init` again reads the saved file and restores the full configuration without any prompts.

```bash
# On a fresh machine — no wizard, config applied directly
git clone ...
cd my-app
lerd init
```

Use `--fresh` to re-run the wizard while keeping existing values as defaults:

```bash
lerd init --fresh
```

---

## Domain naming

Directories with real TLDs are automatically normalised — dots are replaced with dashes and the TLD is stripped before appending `.test`.

For example: `admin.astrolov.com` → `admin-astrolov.test`

---

## Multiple domains

A site can respond to multiple domains. The argument to `lerd link` is the domain name without the `.test` TLD — it is appended automatically from the global config.

```bash
lerd link myapp                # links as myapp.test
```

After linking, you can add more domains:

```bash
lerd domain add api            # adds api.test
lerd domain add admin          # adds admin.test
lerd domain list
#   myapp.test (primary)
#   api.test
#   admin.test
lerd domain remove api         # removes api.test
```

Domains are stored in `.lerd.yaml` as an array (without the TLD) so the file stays portable across machines with different TLD configurations:

```yaml
domains:
  - myapp
  - admin
```

You can also manage domains from the web UI — click the pencil icon next to the domain in the site header to open the domain management modal.

When a site is secured with HTTPS, the certificate is automatically reissued to cover all domains.

Subdomains (e.g. `anything.myapp.test`) are automatically routed to the same site. Git worktree subdomains take priority when they exist.

---

## Workers

The `lerd init` wizard includes a workers step that lets you select which workers to auto-start when linking. Available workers depend on the framework and what's installed:

- **queue** — shown when the framework defines a queue worker (replaced by horizon when `laravel/horizon` is installed)
- **horizon** — shown only when `laravel/horizon` is in `composer.json`
- **schedule** — the task scheduler
- **reverb** — shown only when `laravel/reverb` is installed or `BROADCAST_CONNECTION=reverb` is in `.env`
- **custom workers** — any additional workers defined in the framework definition

Selected workers are saved to `.lerd.yaml`:

```yaml
workers:
  - horizon
  - schedule
```

When `lerd link` runs and workers are configured but not yet running, it prompts to run `lerd setup` so you can install dependencies, run migrations, and start workers in the right order. If workers are already running (re-link), they are left as-is.

`lerd setup` pre-selects worker steps based on the `.lerd.yaml` workers list. Workers not in the list still appear in the step selector but are unchecked.

Toggling workers from the CLI (`lerd queue:start`, `lerd schedule:stop`, etc.) or the web UI syncs the running state back to `.lerd.yaml` when the file exists.

`lerd check` validates that listed workers are valid for the detected framework.

`lerd status` includes a Workers section showing all active, restarting, or failed workers across sites. In the web UI, failing workers show a pulsing red toggle and their log tab appears with a "!" indicator.

---

## Name collision handling

When a directory is parked or linked and another site is already registered with the same name:

- **Same path** — treated as a re-link of the same site. The existing registration is updated and the TLS state is preserved.
- **Different path** — the new site is registered with a numeric suffix (`myapp-2`, `myapp-3`, …) so both sites can coexist.

---

## Linking from the web UI

You can link a new site directly from the dashboard by clicking the **+** button in the sites panel header. A directory browser modal lets you navigate to the project folder and click **Link This Directory**. After linking, the site's `.env` is auto-configured and the UI switches to the new site's settings.

---

## Unlinked domains

When you visit a `.test` domain that isn't linked to any site, lerd shows a branded "Site Not Found" page with a link to the dashboard and a retry button. This replaces the browser's generic connection error.

---

## Unlink behaviour

When you unlink a site that lives inside a parked directory, the vhost is removed but the registry entry is kept and marked as *ignored* — the watcher will not re-register it on its next scan. Running `lerd link` in that directory clears the ignored flag and restores the site.

---

## Pausing sites

Pausing a site frees up resources without removing it from lerd. It is useful when you're switching focus between projects and want to stop workers and silence a site without fully unlinking it.

```bash
lerd pause              # pause the site in the current directory
lerd pause my-project   # pause a named site
```

When a site is paused:

- All running workers for that site are stopped (queue, schedule, reverb, stripe, and any custom workers)
- The nginx vhost is replaced with a minimal landing page that shows a **Resume** button
- Services no longer needed by any other active site are auto-stopped
- The paused state is persisted — the site stays paused across `lerd start` / `lerd stop` cycles

The landing page's **Resume** button calls the lerd dashboard API directly, so you can unpause from the browser without opening a terminal.

```bash
lerd unpause              # resume the site in the current directory
lerd unpause my-project   # resume a named site
```

When a site is unpaused:

- The original nginx vhost is restored (including HTTPS if the site is secured)
- Any services referenced in the site's `.env` are started
- Workers that were running before the pause are restarted

Paused sites still appear in `lerd sites` output and the web UI. Their status is shown as `paused`.

### Running CLI commands on a paused site

You can run `php artisan`, `composer`, `lerd db:export`, and other exec-based commands on a paused site without unpausing it first. If any services the site needs (MySQL, Redis, etc.) were auto-stopped when the site was paused, lerd starts them automatically before running the command:

```
$ php artisan migrate
[lerd] site "my-project" is paused — starting required services...
  Starting mysql...

   INFO  Nothing to migrate.
```

On subsequent commands the services are already running, so no notice is printed. The site stays paused — the nginx vhost remains as the landing page and workers are not restarted.

Commands that benefit from this auto-start:

| Command | Notes |
|---|---|
| `php artisan <args>` / `lerd artisan <args>` | Any artisan command |
| `php <args>` / `lerd php <args>` | Any PHP script |
| `composer <args>` | Composer via the lerd shim |
| `lerd shell` | Opens an interactive shell in the PHP-FPM container |
| `lerd db:import` | Imports a SQL dump |
| `lerd db:export` | Exports a database |
| `lerd db:shell` | Opens an interactive DB shell |

---

## Git worktrees

Lerd automatically creates a subdomain for each `git worktree` checkout. See [Git Worktrees](../features/git-worktrees.md) for details.

---

## Sharing sites

`lerd share` exposes the current site via a public tunnel. Requires [ngrok](https://ngrok.com/download), [cloudflared](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/), or [Expose](https://expose.dev) to be installed.

| Command | Description |
|---|---|
| `lerd share` | Share the current site (auto-detects ngrok, cloudflared, or Expose) |
| `lerd share <name>` | Share a named site |
| `lerd share --ngrok` | Force ngrok |
| `lerd share --cloudflare` | Force Cloudflare Tunnel (cloudflared) |
| `lerd share --expose` | Force Expose |
| `lerd share --localhost-run` | Force localhost.run (SSH, no signup) |
| `lerd share --serveo` | Force serveo.net (SSH, no signup) |

A local reverse proxy rewrites the `Host` header to the site's domain so nginx routes to the correct vhost. Response `Location` headers and HTML/CSS/JS/JSON body references to the local domain are also rewritten to the public tunnel URL, so redirects and asset links work correctly in the browser.
