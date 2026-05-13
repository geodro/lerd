# Dump viewer

`dump()` and `dd()` are the fastest way to inspect a value in PHP, but the output gets lost the moment it ships through Blade, a queue worker, or an XHR response. lerd's dump viewer captures every `dump()` / `dd()` call and streams it to the dashboard, the System sidebar, the TUI, and the MCP tools, so the value is always one click away even when the response itself isn't readable.

![Dump viewer on a site's Dumps tab](/assets/screenshots/site-detail-dumps.png)

The feature is **off by default**. Enable it with `lerd dump on`, the antenna toggle in the Sites sidebar, the Enable button on a per-site Dumps tab, or `dumps_toggle` via MCP. All of these flip the same global flag.

## How it works

The bridge is always mounted into every PHP-FPM container regardless of the toggle state:

- `/usr/local/etc/lerd/dump-bridge.php` — a small PHP file that, when active, defines `dump()` and `dd()` (taking precedence over Symfony's stock helpers via `function_exists` guards) and ships each cloned variable as newline-delimited JSON to lerd-ui.
- `/usr/local/etc/php/conf.d/97-lerd-dump.ini` — sets `auto_prepend_file=...dump-bridge.php` so the bridge is loaded before every request.
- `/usr/local/etc/lerd/enabled.flag` — runtime sentinel. The bridge's first line is `file_exists('/usr/local/etc/lerd/enabled.flag') || return;`. Present file = capture is on, absent file = the bridge is a fast no-op (one stat call per request, no functions overridden).

Toggling the bridge writes or removes that sentinel file. **No FPM container restart, no worker cascade, no quadlet rewrite.** The bridge file and its conf.d ini stay mounted whether or not captures are active.

The receiver's transport depends on the host:

- **Linux** — a per-user Unix socket bound by `lerd-ui` at `~/.local/share/lerd/run/lerd-dumps.sock`. PHP-FPM containers reach it via the existing `%h:%h` bind mount. No host TCP listener, no LAN exposure.
- **macOS** — TCP loopback `127.0.0.1:9913`. Unix sockets don't traverse the podman-machine virtio-fs boundary as functional sockets, so FPM senders inside the VM reach `lerd-ui` on the host via `host.containers.internal:9913` (gvproxy forwards that upstream).

`lerd-ui` buffers the last 500 events in memory and fans them out to four surfaces:

- **Web dashboard** — three places:
  - Each site detail pane has a **Dumps** tab next to Overview and Tinker, pre-filtered to that site.
  - **System > Dump bridge** opens a global view with the listener address, the buffered count, an Enable/Disable button, and every dump across every project.

    ![System Dump bridge detail](/assets/screenshots/system-dump-bridge.png)

  - The Sites list header has a small antenna toggle. Pulsing emerald dot when capturing, grey when off.
  - The System Health card on the dashboard shows the bridge state alongside DNS / nginx / watcher.
- **TUI** — press **D** in `lerd tui` to swap the detail pane for the live dump feed (global).
- **CLI** — `lerd dump tail` streams events to your terminal, with `--site` and `--ctx` filters.
- **MCP** — `dumps_recent`, `dumps_status`, `dumps_clear`, `dumps_toggle` for AI-agent access.

## Wire format

Each event is one line of JSON. The shape is stable from v1 of the protocol:

```json
{
  "v": 1,
  "id": "...ULID...",
  "ts": "2026-05-10T12:34:56.123Z",
  "kind": "dump",
  "ctx": {
    "type": "fpm",
    "site": "acme",
    "branch": "feat-a",
    "domain": "acme.test",
    "request": "GET /users/42",
    "pid": 1234
  },
  "src": { "file": "/home/u/Code/acme/app/Http/Controllers/X.php", "line": 84 },
  "label": "user",
  "text": "App\\Models\\User {#42 ...}"
}
```

`ctx.branch` is set when the dump came from a worktree, so a request to `feat-a.acme.test` carries `branch: "feat-a"` alongside the parent `site: "acme"`. Plumbed end-to-end via `LERD_SITE` and `LERD_BRANCH` fastcgi params on the worktree vhost, so the dashboard renders a branch chip per row, the CLI tail prints `<site>/<branch>` in the header, and TUI / MCP filters can scope by branch. Parent-site requests leave the field empty.

Reserved fields: `tree` (structured cloner output, populated in a future revision) and `trunc` (set to `true` when the cloner output exceeded the per-event cap).

## CLI

| Command | What it does |
| --- | --- |
| `lerd dump on` | Touch the sentinel; the next PHP request captures into the dashboard. |
| `lerd dump off` | Remove the sentinel; subsequent requests are no-ops. |
| `lerd dump status` | Print enabled/disabled, listener address, buffered count. |
| `lerd dump tail [--site X] [--ctx fpm\|cli]` | Stream events to the terminal until Ctrl-C. |
| `lerd dump clear` | Clear the in-memory ring without disabling the bridge. |

None of these commands restart any FPM container or worker.

## Caveats

- **Only `dump()` / `dd()` are intercepted** in this revision. Eloquent queries, jobs, blade renders, and outgoing HTTP requests are not captured (planned for follow-up work).
- **Response output is suppressed by default.** While the bridge is on, `dump()` and `dd()` ship to the dashboard only, the HTTP response stays clean. If you'd rather keep the original `sf-dump` output in the response too (useful as a fallback when `lerd-ui` isn't running), flip the "Also print to response (passthrough)" toggle on **System > Dump bridge**, or set `dumps.passthrough: true` in `~/.config/lerd/config.yaml`. Passthrough is read at PHP-FPM startup, so toggling it via the UI restarts every `lerd-php*-fpm` unit; editing the config file by hand requires a manual restart for the change to take effect.
- **VarCloner caps.** Defaults are `setMaxItems(2500)` and `setMaxString(4096)`. Override via `LERD_DUMP_MAX_ITEMS` in the site's `.env`.
- **Loopback only.** On Linux the receiver binds a per-user Unix socket under `~/.local/share/lerd/run/lerd-dumps.sock` (no host TCP listener). On macOS it binds `127.0.0.1:9913` — reachable from FPM inside podman-machine via gvproxy's `host.containers.internal:9913` mapping, not from the LAN.
- **No persistence.** Buffer is in-memory only and resets when `lerd-ui` restarts.
- **First upgrade restarts FPM once.** Existing installs that update to v1.20 will see their FPM `.container` files rewritten on the next `lerd install` / `lerd start` to add the always-mounted bridge volumes. Every subsequent toggle is restart-free.
