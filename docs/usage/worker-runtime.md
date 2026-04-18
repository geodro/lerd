# Worker runtime (macOS)

On macOS, lerd can launch framework workers (queue, schedule, horizon, reverb, and any custom framework workers) in one of two shapes. The choice is a memory-versus-supervision tradeoff.

On Linux lerd always uses the exec shape, because systemd is a dependable supervisor for `podman exec`. The setting is informational there and doesn't change behaviour.

## The two modes

### `exec` (default)

One `podman exec` process per worker, supervised by launchd. All workers for a site share the project's PHP-FPM container, so they reuse OPcache, the loaded `vendor/` tree, and the parent PHP process's memory pages.

- **Memory**: near-zero extra per worker. Shared with the FPM pool.
- **Supervision**: launchd watches the outer `podman exec`. Brief podman-machine SSH bridge hiccups can make launchd think the exec died when the inner worker is still running. To prevent duplicates, lerd wraps each worker in a pid-file guard script. If a previous process is still alive when launchd relaunches, the new invocation exits cleanly and launchd waits for the real process to exit before trying again.
- **Matches Linux**: same architecture and memory profile as Linux under systemd.

### `container`

One detached container per worker, spawned from the shared FPM image. Every worker has its own container name (e.g. `lerd-horizon-astrolov`), its own process namespace, and its own PHP interpreter.

- **Memory**: one full PHP process per worker. Horizon is especially costly because it supervises its own pool of PHP workers. A site with queue + schedule + horizon + reverb + a couple of custom framework workers can add 5-7 extra containers.
- **Supervision**: 1:1 between podman's `--restart=always` and the worker. No launchd-level race, no SSH bridge ambiguity.

## Switching modes

### Via CLI

```sh
lerd workers mode            # show current mode
lerd workers mode exec       # use shared FPM container (lower memory)
lerd workers mode container  # use per-worker containers (higher reliability)
```

### Via the terminal dashboard

Open `lerd tui`, press `S` for Settings, move the cursor onto the worker mode row, press `space` to toggle.

### Via the web UI

`GET /api/settings` returns `worker_exec_mode` (`exec` or `container`) alongside `worker_mode_applies` (`true` only on macOS). `POST /api/settings/worker-mode` with `{"mode": "exec" | "container"}` flips the setting. A future dashboard release exposes a UI control for this.

## Applying the change

On macOS, toggling the mode runs a scoped migration automatically: every active worker is stopped in its old shape, the stale on-disk artifacts (quadlet, service unit, guard script, pid file, leftover container) are cleaned up, and the worker restarts in the new shape. Other lerd surfaces (FPM, nginx, DNS, built-in services) are untouched, so the operation is fast and localised.

If a single worker fails to restart (e.g. the framework definition is missing), a warning is printed and the migration continues with the rest. Re-running `lerd stop && lerd start` always recovers.

On Linux the setting is informational, so no migration runs there.

## Which to choose

- **Default to `exec`** on a machine with limited RAM (8 GB or 16 GB MacBooks especially) or a site count where the container-per-worker model would add up to a gigabyte of headroom lost to worker containers. This is the current default.
- **Switch to `container`** if you see phantom or duplicate workers under heavy load, if your podman machine's SSH bridge is observably flaky, or if you want per-worker log isolation via `podman logs -f lerd-horizon-<site>`.

The choice is reversible at any time and doesn't affect site configuration.
