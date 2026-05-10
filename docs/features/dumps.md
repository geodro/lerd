# Dump viewer

`dump()` and `dd()` are the fastest way to inspect a value in PHP, but the output gets lost the moment it ships through Blade, a queue worker, or an XHR response. lerd's dump viewer captures every `dump()` / `dd()` call and streams it to the dashboard, the TUI, and the MCP tools, so the value is always one click away even when the response itself isn't readable.

The feature is **off by default**. Enable it for the current install with `lerd dump on`, and disable it again with `lerd dump off`. Toggling rewrites every PHP-FPM container quadlet and restarts the affected units; the bridge is a thin shared PHP file mounted into each container.

## How it works

Once enabled, lerd ships two files into every PHP-FPM container:

- `/usr/local/etc/lerd/dump-bridge.php` — a small PHP file that defines `dump()` and `dd()` (taking precedence over Symfony's stock helpers via `function_exists` guards), and on each call ships the cloned variable as newline-delimited JSON over TCP to `host.containers.internal:9913`.
- `/usr/local/etc/php/conf.d/97-lerd-dump.ini` — sets `auto_prepend_file=...dump-bridge.php` so the bridge is loaded before every request.

The receiver is a loopback TCP listener that runs inside `lerd-ui`. It buffers the last 500 events in memory and fans them out to four surfaces:

- **Web dashboard** — the **Dumps** tab in the side nav. Grouped by request, filterable by site and context, with a free-text search.
- **TUI** — press **D** in `lerd tui` to swap the detail pane for the live dump feed.
- **CLI** — `lerd dump tail` streams events to your terminal, with `--site` and `--ctx` filters.
- **MCP** — `dumps_recent`, `dumps_status`, `dumps_clear`, `dumps_toggle` for AI-agent access.

The receiver listens unconditionally as long as `lerd-ui` is running; the toggle controls only whether FPM containers actually have the bridge mounted. That means turning the bridge on is instant and doesn't require a UI restart.

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
    "domain": "acme.test",
    "request": "GET /users/42",
    "pid": 1234
  },
  "src": { "file": "/home/u/Code/acme/app/Http/Controllers/X.php", "line": 84 },
  "label": "user",
  "text": "App\\Models\\User {#42 ...}"
}
```

Reserved fields: `tree` (structured cloner output, populated in a future revision) and `trunc` (set to `true` when the cloner output exceeded the per-event cap).

## CLI

| Command | What it does |
| --- | --- |
| `lerd dump on` | Enable the bridge; writes the assets and restarts each FPM unit. |
| `lerd dump off` | Disable; strips the volumes from each FPM quadlet, restarts, removes the host assets. |
| `lerd dump status` | Print the current state plus a buffered-event count from lerd-ui. |
| `lerd dump tail [--site X] [--ctx fpm\|cli]` | Stream events to the terminal until Ctrl-C. |
| `lerd dump clear` | Clear the in-memory ring without disabling the bridge. |

## Caveats

- **Only `dump()` / `dd()` are intercepted** in this revision. Eloquent queries, jobs, blade renders, and outgoing HTTP requests are not captured (planned for follow-up work).
- **Original output is still emitted.** The bridge calls Symfony's VarDumper after sending its own copy, so Whoops/Ignition/HTTP responses still show the dump in the page. Use `lerd dump off` if that's undesirable for a particular session.
- **VarCloner caps.** Defaults are `setMaxItems(2500)` and `setMaxString(4096)`. Override via `LERD_DUMP_MAX_ITEMS` in the site's `.env`.
- **One TCP port.** The receiver binds `127.0.0.1:9913`. If another process is already listening there, the receiver logs a warning at startup and the dashboard shows an empty feed; nothing else breaks.
- **No persistence.** Buffer is in-memory only and resets when `lerd-ui` restarts.
