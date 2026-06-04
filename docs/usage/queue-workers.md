# Queue Workers & Framework Workers

Lerd can run framework-defined workers as persistent systemd user services. Workers run inside the project's PHP-FPM container and restart automatically on failure.

## Queue worker

| Command | Description |
|---|---|
| `lerd queue:start` | Start the queue worker for the current project |
| `lerd queue:stop` | Stop the queue worker for the current project |
| `lerd queue start` | Same as `queue:start` (subcommand form) |
| `lerd queue stop` | Same as `queue:stop` (subcommand form) |

Works for any framework that defines a `queue` worker. Laravel has it built-in (`php artisan queue:work`).

---

## Laravel Horizon

If `laravel/horizon` is present in `composer.json`, lerd detects it automatically and switches to Horizon mode:

- The queue toggle in the web UI is replaced by a **Horizon** toggle
- Use `lerd horizon:start` / `lerd horizon:stop` instead of `queue:start` / `queue:stop`

| Command | Description |
|---|---|
| `lerd horizon:start` | Start Horizon for the current project as a systemd service |
| `lerd horizon:stop` | Stop Horizon for the current project |
| `lerd horizon:reload [on\|off]` | Toggle auto-reload on file changes (prints the current state with no argument) |
| `lerd horizon start` | Same as `horizon:start` (subcommand form) |
| `lerd horizon stop` | Same as `horizon:stop` (subcommand form) |
| `lerd horizon reload [on\|off]` | Same as `horizon:reload` (subcommand form) |

Horizon manages its own worker pools via `config/horizon.php` and does not accept `--queue`, `--tries`, or `--timeout` flags. Those are configured in the Horizon config file instead.

The systemd unit is named `lerd-horizon-{sitename}`. Logs:
```bash
journalctl --user -u lerd-horizon-my-app -f
```

### Auto-reload on file changes

By default lerd runs `php artisan horizon`, which boots the app once and caches your code, so after editing a job, listener, or any class a worker touches you have to restart Horizon for the change to take effect.

Turn on auto-reload to run `php artisan horizon:listen` instead. Horizon then watches your project and restarts its workers automatically whenever a file changes, so you never stop/restart Horizon by hand while developing. The dashboard and `config/horizon.php` keep working exactly the same.

```bash
lerd horizon:reload on    # use horizon:listen (auto-restart on file changes)
lerd horizon:reload off   # back to standard horizon
lerd horizon:reload       # show the current state
```

Auto-reload is off by default. The preference is per project, stored as `reload_workers` in the project's `.lerd.yaml` (a list of worker names opted into reload mode, currently just `horizon`), so one project can develop with auto-reload while another stays in standard mode. In the dashboard the Horizon toggle and the reload toggle sit together as one grouped control, and the reload toggle only appears while Horizon is running, since reload is a property of a live worker. Either way, the running Horizon worker for the project is restarted so the change applies immediately, and the new state is pushed to every open dashboard over the websocket.

Two notes:

- The watcher shells out to Node and resolves [`chokidar`](https://www.npmjs.com/package/chokidar) from your project's `node_modules`. Horizon ships the watcher script but not chokidar itself, so the project has to provide it. It used to arrive for free as a transitive dependency of Vite, but Vite 8 dropped it, so a plain `npm install` is no longer enough. If chokidar is missing, the toggle never silently reads as on: from the dashboard, enabling pops a modal that offers a one-click `npm install --save-dev chokidar` and then turns reload on once the watcher is present; from the CLI, `lerd horizon:reload on` refuses with the same `npm install -D chokidar` hint.
- lerd adds `--poll` where the container can't see host filesystem events: on macOS, where workers run in the podman virtual machine, and under WSL2, where projects on `/mnt` (9p) mounts get no inotify delivery. On native Linux the container shares the host filesystem directly and inotify works, so polling is left off to avoid the wasted CPU.

## Generic workers (`lerd worker`)

Use this for any other framework-defined worker:

| Command | Description |
|---|---|
| `lerd worker start <name>` | Start a named worker for the current project |
| `lerd worker stop <name>` | Stop a named worker |
| `lerd worker list` | List all workers defined for this project's framework |

Example, start the Symfony Messenger consumer:
```bash
lerd worker start messenger
# Systemd unit: lerd-messenger-myapp.service
# Logs: journalctl --user -u lerd-messenger-myapp -f
```

Workers are defined in framework YAML definitions at `~/.config/lerd/frameworks/`. See [Frameworks](frameworks.md) for how to add custom workers to any framework.

---

## Options for `queue:start`

| Flag | Default | Description |
|---|---|---|
| `--queue` | `default` | Queue name to process |
| `--tries` | `3` | Max attempts before marking a job as failed |
| `--timeout` | `60` | Seconds a job may run before timing out |

---

## Redis requirement

If `QUEUE_CONNECTION=redis` is set in the project's `.env`, lerd verifies that `lerd-redis` is running before starting the worker. If it is not, you will see:

```
queue worker requires Redis (QUEUE_CONNECTION=redis in .env) but lerd-redis is not running
Start it first: lerd services start redis
```

---

## Example

```bash
cd ~/Lerd/my-app
lerd queue:start --queue=emails,default --tries=5 --timeout=120
# Systemd unit: lerd-queue-my-app.service
# Logs: journalctl --user -u lerd-queue-my-app -f
```

---

## Worker state in `.lerd.yaml`

Every start/stop command (`queue:start`, `queue:stop`, `horizon:start`, `schedule:start`, `reverb:start`, `stripe:listen`, `worker start`, etc.) automatically updates the `workers` list in `.lerd.yaml` when the file exists. This means:

- Cloning a project and running `lerd link` or `lerd setup` restores all workers.
- After an uninstall/reinstall cycle, `lerd start` reads `.lerd.yaml` and recreates missing worker units automatically, no need to re-run each start command manually.

The `workers` field is maintained automatically. You do not need to edit it by hand.

---

## Auto-restart on config changes

The lerd watcher daemon monitors `.env`, `composer.json`, `composer.lock`, and `.php-version` for every registered site. When any of those files change it:

- Signals `php artisan queue:restart` inside the PHP-FPM container (debounced to 2 seconds)
- If `.php-version` changed: updates the site registry and regenerates the nginx vhost automatically, no manual reload needed

This ensures queue workers and nginx stay in sync after deploys or PHP version changes without manual intervention.

---

## Failing and restarting workers

`lerd status` includes a Workers section that lists all active, restarting, or failed workers across sites. Paused sites are excluded from this list.

Workers that are crash-looping (repeatedly failing and restarting) are detected automatically. When you unlink a site, lerd stops any crash-looping workers for that site to prevent them from consuming resources after the site is gone.

In the web UI, a failing worker shows a pulsing red toggle and its log tab appears with a **!** indicator so you can inspect the error output immediately.

---

## Web UI control

Queue workers and Horizon are controllable from the **Sites tab** in the web UI:

- For projects **without** Horizon: an amber **Queue** toggle starts or stops the queue worker.
- For projects **with** `laravel/horizon` installed: the Queue toggle is replaced by a **Horizon** toggle (auto-detected from `composer.json`).

When a worker is running, a log tab (**Queue** or **Horizon**) appears in the site detail panel alongside PHP-FPM. The amber dot next to the site in the sidebar indicates a worker is active.
