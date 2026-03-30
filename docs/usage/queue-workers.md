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
| `lerd horizon start` | Same as `horizon:start` (subcommand form) |
| `lerd horizon stop` | Same as `horizon:stop` (subcommand form) |

Horizon manages its own worker pools via `config/horizon.php` and does not accept `--queue`, `--tries`, or `--timeout` flags — those are configured in the Horizon config file instead.

The systemd unit is named `lerd-horizon-{sitename}`. Logs:
```bash
journalctl --user -u lerd-horizon-my-app -f
```

## Generic workers (`lerd worker`)

Use this for any other framework-defined worker:

| Command | Description |
|---|---|
| `lerd worker start <name>` | Start a named worker for the current project |
| `lerd worker stop <name>` | Stop a named worker |
| `lerd worker list` | List all workers defined for this project's framework |

Example — start the Symfony Messenger consumer:
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

## Auto-restart on config changes

The lerd watcher daemon monitors `.env`, `composer.json`, `composer.lock`, and `.php-version` for every registered site. When any of those files change it:

- Signals `php artisan queue:restart` inside the PHP-FPM container (debounced to 2 seconds)
- If `.php-version` changed: updates the site registry and regenerates the nginx vhost automatically — no manual reload needed

This ensures queue workers and nginx stay in sync after deploys or PHP version changes without manual intervention.

---

## Web UI control

Queue workers and Horizon are controllable from the **Sites tab** in the web UI:

- For projects **without** Horizon: an amber **Queue** toggle starts or stops the queue worker.
- For projects **with** `laravel/horizon` installed: the Queue toggle is replaced by a **Horizon** toggle (auto-detected from `composer.json`).

When a worker is running, a log tab (**Queue** or **Horizon**) appears in the site detail panel alongside PHP-FPM. The amber dot next to the site in the sidebar indicates a worker is active.
