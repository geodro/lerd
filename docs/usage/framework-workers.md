# Framework Workers

Every framework can define long-running workers (queue consumers, schedulers, WebSocket servers). This page covers the worker commands, conditional rules, conflicts, proxy wiring, project-specific custom workers, and orphan cleanup.

Each framework can define **workers**: long-running processes managed as systemd user services inside the PHP-FPM container.

| Command | Description |
|---|---|
| `lerd worker start <name>` | Start a named worker for the current project |
| `lerd worker stop <name>` | Stop a named worker |
| `lerd worker list` | List all workers defined for this project's framework |

The shortcut commands `lerd queue:start`, `lerd schedule:start`, `lerd reverb:start`, and `lerd horizon:start` are aliases; they look up the worker from the framework definition and delegate to the generic handler. They work for any framework that defines a worker with that name.

## Worker features

**Conditional workers**: Workers with a `check` rule only appear when the condition passes (e.g. `laravel/horizon` is in `composer.json`):

```yaml
workers:
  horizon:
    command: php artisan horizon
    check:
      composer: laravel/horizon
```

**Conflict resolution**: Workers can declare conflicts. When a conflicting worker starts, the other is stopped automatically and hidden from the UI:

```yaml
workers:
  horizon:
    command: php artisan horizon
    conflicts_with:
      - queue      # stops queue before starting horizon; hides queue toggle in UI
```

**WebSocket/HTTP proxy**: Workers that need an nginx proxy block define a `proxy` config. Lerd auto-assigns a collision-free port and regenerates the nginx vhost:

```yaml
workers:
  reverb:
    command: php artisan reverb:start
    proxy:
      path: /app                    # URL path for the proxy location block
      port_env_key: REVERB_SERVER_PORT  # env key holding the port
      default_port: 8080            # starting port for auto-assignment
```

Port assignment scans all proxy port env keys across all sites to prevent collisions between different workers and frameworks.

## Project-specific custom workers

Add workers to `.lerd.yaml` for project-specific needs that don't belong in the framework definition:

```yaml
# .lerd.yaml
framework: symfony
framework_version: "8"
workers:
  - messenger
  - pdf-generator
custom_workers:
  pdf-generator:
    label: PDF Generator
    command: php bin/console app:generate-pdfs --daemon
    restart: always
```

Custom workers with proxy support:

```yaml
custom_workers:
  mercure:
    label: Mercure Hub
    command: php bin/console mercure:run
    restart: always
    proxy:
      path: /.well-known/mercure
      port_env_key: MERCURE_PORT
      default_port: 3000
```

Custom workers are merged with the framework's workers at runtime. They are committed to git so teammates get the same setup.

## Worker logs

```bash
journalctl --user -u lerd-messenger-myapp -f
```

## Managing custom workers

Use `lerd worker add` to add project-specific or global custom workers without manually editing YAML:

```bash
# Add a project-specific worker (saved to .lerd.yaml)
lerd worker add pulse --command "php artisan pulse:work" --label "Pulse" --check-composer laravel/pulse

# Add a worker that conflicts with another (stops it on start, hides it in UI)
lerd worker add custom-queue --command "php artisan queue:work --queue=emails" --conflicts-with queue

# Add a global worker (saved to ~/.config/lerd/frameworks/<name>.yaml)
lerd worker add pulse --command "php artisan pulse:work" --global

# Remove a custom worker (stops it if running)
lerd worker remove pulse
lerd worker remove pulse --global
```

Project workers (`.lerd.yaml`) apply to a single project and are committed to git. Global workers (user overlay) apply to all projects using that framework. Both survive framework store updates.

The resulting `.lerd.yaml` looks like:

```yaml
framework: laravel
custom_workers:
  pulse:
    label: Pulse
    command: php artisan pulse:work
    check:
      composer: laravel/pulse
  custom-queue:
    command: php artisan queue:work --queue=emails
    conflicts_with:
      - queue
```

After adding, start the worker with `lerd worker start pulse`.

When running `lerd init --fresh`, existing custom workers are shown in a multi-select step before the workers step. Deselecting a custom worker removes it from `.lerd.yaml` and excludes it from the workers selection. If the removed worker had `conflicts_with`, those workers become available again.

## Orphaned workers

A worker becomes orphaned when its systemd unit is still running but its definition has been removed from `.lerd.yaml` (e.g. after a `git pull` or manual edit). Orphaned workers are detected and surfaced in several places:

- **`lerd worker list`**: shows orphaned workers with a stop hint
- **`lerd worker stop <name>`**: can stop orphaned workers even without a definition
- **`lerd setup`**: offers orphaned workers as pre-selected stop steps before framework worker starts
- **UI**: the stop button works for orphaned workers directly

## Web UI (worker toggles)

Framework workers appear as toggles in the Sites panel. Workers with a `check` rule only appear when the condition passes. Workers with `conflicts_with` suppress each other (e.g. when Horizon is available, the queue toggle is hidden).

Custom framework workers from `.lerd.yaml` also appear as toggles alongside the framework's standard workers.

---

See also: [Frameworks](frameworks.md) for the framework store and Laravel definition; [Framework definitions](framework-definitions.md) for the YAML schema.
