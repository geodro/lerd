# Site Management

## Commands

| Command | Description |
|---|---|
| `lerd init` | Interactive wizard — choose PHP version, HTTPS, and services, then save `.lerd.yaml` and apply |
| `lerd init --fresh` | Re-run the wizard with existing `.lerd.yaml` values as defaults |
| `lerd park [dir]` | Register all Laravel projects inside `dir` (defaults to cwd) |
| `lerd unpark [dir]` | Remove a parked directory and unlink all its sites |
| `lerd link [name]` | Register the current directory as a site |
| `lerd link [name] --domain foo.test` | Register with a custom domain |
| `lerd unlink [name]` | Stop serving the site |
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

`lerd init` runs an interactive wizard that asks you three questions, writes the answers to `.lerd.yaml` in the project root, and then applies the configuration — linking the site, enabling HTTPS if requested, and starting any required services.

```bash
cd ~/Projects/my-app
lerd init
```

```
? PHP version: 8.4
? Enable HTTPS? No
? Services needed:
  ◉ mysql
  ◉ redis
  ◯ postgres
  ◯ meilisearch
  ◯ rustfs
  ◯ mailpit
Saved .lerd.yaml
Linked: my-app -> my-app.test (PHP 8.4, Node 22, Framework: laravel)
```

Wizard defaults are populated intelligently on first run:

- **PHP version** — from the site registry if already linked, otherwise from `.php-version`, `composer.json`, or the global default
- **Enable HTTPS** — pre-checked if the site is already secured
- **Services** — pre-checked based on what's detected in the project's `.env` file

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

## Name collision handling

When a directory is parked or linked and another site is already registered with the same name:

- **Same path** — treated as a re-link of the same site. The existing registration is updated and the TLS state is preserved.
- **Different path** — the new site is registered with a numeric suffix (`myapp-2`, `myapp-3`, …) so both sites can coexist.

---

## Unlink behaviour for parked sites

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
