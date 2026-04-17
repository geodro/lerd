# Start, Stop & Autostart

Day-to-day lifecycle commands for the entire lerd stack: DNS, nginx, PHP-FPM containers, services, workers, the Web UI, the watcher, and the system tray.

::: tip You don't need to run `lerd start` after installing
`lerd install` already starts everything for you on first run: it boots `lerd-dns`, `lerd-nginx`, the `lerd-watcher`, and the system tray. Services like MySQL or Redis are started on demand the first time something needs them (`lerd service start`, `lerd init`, or `lerd env`). Reach for `lerd start` only after a `lerd stop`, a reboot without autostart enabled, or after you've manually killed containers.
:::

---

## Commands at a glance

| Command | Stops | Starts |
|---|---|---|
| `lerd start` | nothing | DNS, nginx, watcher, tray, all PHP-FPM containers in use, services that were running before stop, queue / schedule / reverb / messenger workers, stripe listeners, Web UI |
| `lerd stop` | All containers and workers above. Leaves the watcher and Web UI alone. | nothing |
| `lerd quit` | Everything `lerd stop` does, **plus** the Web UI, watcher, and tray. macOS: also stops the Podman Machine VM. | nothing |

`lerd stop` is the everyday "give my laptop back its CPU" command. `lerd quit` is a full shutdown: use it before a reinstall, a system reboot without autostart, or when you really want lerd out of the way.

---

## `lerd start`

```bash
lerd start
```

Walks the install in dependency order:

1. Pre-flight: checks for **port conflicts** on 53, 80, and 443; refuses to start if another process is bound.
2. Rebuilds or pulls any missing container images (e.g. after a `podman rmi` or a podman cleanup).
3. Boots core: `lerd-dns`, `lerd-nginx`, `lerd-watcher`.
4. Boots every PHP-FPM container that has at least one site referencing its version. Unused PHP versions stay stopped.
5. Boots all installed services that are **not** marked as manually paused (see [Manually stopped services](services.md#manually-stopped-services) for the pause-state contract).
6. Restores per-site workers (`lerd-queue-*`, `lerd-schedule-*`, `lerd-reverb-*`, `lerd-messenger-*`, custom workers) and stripe listeners (`lerd-stripe-*`) from the `workers` list saved in each site's `.lerd.yaml`.
7. Starts the Web UI (`lerd-ui`) and the system tray.

A live spinner shows the per-unit progress. If a single SSL vhost references a missing certificate file, lerd switches that site back to HTTP automatically and continues; one broken cert no longer blocks the whole nginx start.

::: info After a reinstall
If you ran `lerd uninstall` and then reinstalled, worker units and service quadlets are recreated by `lerd start` from each site's `.lerd.yaml`. Sites with a committed `.lerd.yaml` come back fully wired up. Sites without one need their workers restarted manually.
:::

---

## `lerd stop`

```bash
lerd stop
```

Stops everything `lerd start` started **except** the Web UI, watcher, and tray; those keep running so the dashboard stays reachable to bring lerd back up.

A few important details:

- **Manually paused services are remembered.** If you stopped Mailpit earlier with `lerd service stop mailpit`, then `lerd stop` + `lerd start` will not bring Mailpit back. The pause flag survives the cycle.
- **Pinned services start anyway.** A `lerd service pin <name>` overrides auto-stop logic; pinned services are always started by `lerd start` regardless of which sites are active.
- **Worker state is preserved.** Workers running before `lerd stop` are restarted by the next `lerd start`; workers you manually stopped stay stopped.

---

## `lerd quit`

```bash
lerd quit
```

The full off-switch:

1. Runs everything `lerd stop` does.
2. Stops `lerd-ui` (Web UI).
3. Stops `lerd-watcher`.
4. Kills the system tray process.
5. **macOS only:** stops the Podman Machine VM.

After `lerd quit` there are no lerd processes left running. On macOS the Podman Machine VM is also shut down, so `lerd start` will bring it back up on the next run. This is the right command before a reinstall, a system reboot, or before pulling a major update.

The system tray's **Quit Lerd** menu item calls `lerd quit`.

---

## Autostart on login

Lerd can boot itself every time you log in. Autostart is a single switch over every lerd-owned systemd user unit on the machine:

- the dashboard (`lerd-ui.service`), project watcher (`lerd-watcher.service`) and system tray (`lerd-tray.service`)
- every container quadlet (`lerd-mysql`, `lerd-nginx`, `lerd-redis`, `lerd-postgres`, `lerd-dns`, `lerd-php*-fpm`, `lerd-mailpit`, `lerd-meilisearch`, `lerd-minio`, `lerd-rustfs`)
- every per-site worker, queue, schedule, horizon, reverb, and stripe-listen unit

```bash
lerd autostart enable      # boot lerd on every login
lerd autostart disable     # stop booting on login
```

`lerd autostart enable` runs `systemctl --user enable` on the full set; `lerd autostart disable` runs the matching `disable`. The dashboard's enabled state is the canonical "is autostart on" indicator surfaced by the UI and tray.

The same toggle also appears in the **System Tray** menu under **Autostart**; see [System Tray](../features/system-tray.md).

The tray unit (`lerd-tray.service`) is wired to `graphical-session.target` and so requires a desktop environment that reaches that target on login: GNOME, KDE Plasma, and any compositor launched through `uwsm` (Omarchy's Hyprland setup included). Bare Hyprland / Sway / i3 launched without `uwsm` won't autostart the tray; see [System Tray, Autostart](../features/system-tray.md#autostart) for the workaround. Every other lerd unit uses `default.target` and is unaffected.

---

## From the Web UI

The dashboard at `http://127.0.0.1:7073` has **Start** and **Stop** buttons in the header:

- **Start** appears only when one or more core services (DNS, nginx, PHP-FPM) are not running. Clicking it calls `lerd start` via the API.
- **Stop** is always visible while lerd is running. Clicking it calls `lerd stop`.
- The tray's **Quit Lerd** menu item calls `lerd quit` (full shutdown including the UI).

These map one-to-one to the CLI commands above, no special UI-only behaviour.

---

## Status & verification

```bash
lerd status
```

Shows a live snapshot: DNS reachability, nginx, PHP-FPM containers, watcher, services, certificate expiry, and LAN exposure. Run it after every `lerd start` to confirm everything is healthy. See [Troubleshooting](../troubleshooting.md) if anything is reported as down.

---

## Cheat sheet

| Situation | Command |
|---|---|
| Just installed lerd | Nothing, `lerd install` already started everything |
| Coming back to your laptop after `lerd stop` | `lerd start` |
| Reboot, autostart disabled | `lerd start` |
| Reboot, autostart enabled | Nothing, happens automatically |
| Free up CPU / RAM during a heavy build | `lerd stop` |
| Full shutdown before a reinstall | `lerd quit` |
| Verify everything's healthy | `lerd status` |
