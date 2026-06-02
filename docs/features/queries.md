# Query viewer

The dump viewer catches `dump()` / `dd()`, but most of what slows a request down never passes through either: the database queries. lerd's query viewer records every SQL statement a request (or an artisan command) runs, with its bindings, duration, and the exact line that fired it, and streams them to the same **Debug** view as dumps, grouped per request with N+1 detection and slow-query flags.

The feature is **off by default**. Enable it from **System → Debug → Queries** with the Enable button. It shares the dump receiver, so there is nothing else to wire up.

## How it works

Unlike the debug bridge, which works by redefining `dump()` from an `auto_prepend_file`, queries live inside the database layer where a prepend can't reach. lerd ships a small first-party Zend extension, **`lerd_devtools`**, compiled into every PHP-FPM image. It uses PHP's `zend_observer` API (PHP 8.0+) to observe `PDOStatement::execute`, `PDO::query`, and `PDO::exec`, capturing the SQL, the bound parameters, the wall-clock duration, and the calling `file:line` from the backtrace. Because it hooks at the engine level it works for any PDO app, framework or not. On PHP 7.x the extension still loads but captures nothing (no `zend_observer`); query capture needs 8.0+.

Capture is gated by the **same** runtime sentinel as the debug bridge, so the whole Debug window is one switch and toggling never restarts FPM:

- The extension and its config ini (`/usr/local/etc/php/conf.d/96-lerd-devtools.ini`) are always present in the image / mounted.
- `/usr/local/etc/lerd/enabled.flag` is the shared runtime sentinel. Both the debug bridge and the extension stat it once per request; present = capture, absent = no-op. There is no separate devtools enable flag — `lerd dump on/off` (or the dashboard Debug toggle) arms both at once. The worker-capture sub-toggle has its own `devtools-workers.flag`.

Events ship over the **same** Unix socket (Linux) or TCP loopback (macOS) the debug bridge uses, so `lerd-ui` buffers them in the same 500-event ring and fans them out through the same SSE stream. The web client filters by `kind` to render the Queries lens.

## What you get

- **Per-request grouping.** Every query is bucketed by the request (or a 5-second window for CLI), with a rollup header showing the query count and total time.
- **N+1 detection.** Queries whose SQL normalizes to the same fingerprint (literals collapsed) are flagged as duplicates; a request with three or more repeats of one shape is flagged **N+1**.
- **Slow-query tagging.** Any single query at or over 100ms is tagged **slow**.
- **Caller, bindings, connection.** Expand a row for the originating `file:line`, the bound parameters, and (when a framework adapter is present) the connection name and read/write type.

## Wire format

Query events reuse the dump event envelope; the kind-specific fields live under `data`:

```json
{
  "v": 1,
  "id": "...",
  "ts": "2026-06-01T12:34:56.123Z",
  "kind": "query",
  "ctx": { "type": "fpm", "site": "acme", "request": "GET /users", "pid": 1234 },
  "src": { "file": "/home/u/Code/acme/app/Models/User.php", "line": 30 },
  "data": {
    "sql": "select * from users where id = ?",
    "bindings": [42],
    "time_ms": 1.42,
    "connection": "mysql",
    "rw_type": "read"
  }
}
```

`connection` and `rw_type` are filled by the Laravel adapter; the engine-level capture always provides `sql`, `bindings`, and `time_ms`. Bindings are captured whether the app passes them to `execute([...])` or binds them one at a time with `bindValue()` / `bindParam()` (the extension buffers per-statement bound values and attaches them on execute), so Doctrine and other libraries that bind individually still show their parameters.

## Laravel adapter (richer capture)

For Laravel apps, the extension loads a small in-app adapter at `Application::boot` (observed at the engine level) that listens to `QueryExecuted`. While it's active the engine-level PDO capture stands down, so Laravel queries come through with data the raw PDO hook can't see:

- **Real bindings** — Laravel binds via `bindValue()`, invisible to the PDO observer; the adapter reads them from the event (formatted with `prepareBindings()`).
- **Connection name** — e.g. `pgsql`, `mysql`.
- **Per-job grouping** — the adapter resets the request id on every `JobProcessing`, so each queued job is its own group instead of a worker's jobs lumping together.

Non-Laravel apps (and queries that run before the framework boots) still fall back to the engine-level PDO capture. The adapter respects the same on/off and worker-capture policy as the engine path, never throws, and emits to the same socket.

Beyond queries, the same adapter feeds additional Debug sub-tabs:

- **Jobs** *(Laravel)* — queued jobs as they finish, with status (processed/failed), connection, and the exception on failure.
- **Views** — every template rendered, with its source path and the top-level data keys passed in.
- **Mail** — outgoing messages captured before send, with subject, recipients, and a sandboxed HTML preview.
- **Cache** *(Laravel)* — hit / miss / write / forget events with the key and store. Framework-internal keys (the queue restart/pause signals, scheduler overlap mutexes, and reverb/horizon/pulse/telescope pub-sub) are filtered out so the tab shows the application's own cache use rather than background machinery — this matters most with worker capture on, where those keys are polled constantly.
- **Events** *(Laravel)* — application and package events dispatched (framework-internal `Illuminate\*` events are filtered out).
- **HTTP** *(Laravel)* — outgoing requests made via Laravel's HTTP client (method, URL, status), so third-party API calls are visible the way queries are.

Each lens groups per request/job, shows the originating app frame with the stack trace and editor links, and is filterable by site and worker command.

## Framework coverage (agnostic seams)

Queries, Mail and Views are captured **agnostically** at the shared library every framework uses, so they are not Laravel-only. Where the Laravel adapter is active it claims these kinds (richer, event-sourced data) and the agnostic seams stand down to avoid double capture; everywhere else the extension observes the library directly and a small framework-neutral collector (`devtools-collector.php`, mounted next to the adapter) extracts the event in PHP:

- **Queries** — observed at the PDO layer, so any PDO app (Symfony/Doctrine, raw PDO, …) gets them with real bindings.
- **Mail** — observed at `Symfony\Component\Mailer\Mailer::send`, the de-facto mail library used directly by Symfony and wrapped by Laravel. The collector reads the `Symfony\Component\Mime\Email` for subject, recipients and body.
- **Views** — observed at `Twig\Environment::render` / `display`, the de-facto Symfony view layer. The collector resolves the on-disk `.twig` source path through Twig's loader the same way Blade's `getPath()` does, and skips the dev-only `@WebProfiler` toolbar.
- **Events** — observed at `Symfony\Component\EventDispatcher\EventDispatcher::dispatch` (the debug `TraceableEventDispatcher` delegates to it, so each dispatch is seen once). The collector keeps application events and drops the framework lifecycle noise (`kernel.*`, `console.*`, and the `Symfony\`/`Twig\`/`Doctrine\` component internals), mirroring the Laravel `Illuminate\*` filter.
- **Jobs** — observed at `Symfony\Component\Messenger\MessageBus::dispatch` (the debug `TraceableMessageBus` delegates to it). Each message dispatched to the bus is recorded with its class; an `Envelope` is unwrapped to the real message. Status is `dispatched` because the bus only signals that a message was queued. This fires in the web request, unlike Laravel's adapter which reports jobs as a worker finishes them, so the two read slightly differently.
- **HTTP** — observed at `Symfony\Component\HttpClient\CurlHttpClient::request` / `NativeHttpClient::request` (the default factory yields one of these; decorators delegate down to them). Captured at the *begin* of the call because `request()` rewrites its own `$url` argument internally. Symfony responses are lazy, so no status code is known at call time — the row shows the method and URL with a `sent` marker rather than a status, whereas Laravel's adapter (which listens after the response arrives) shows the real code.

Cache still comes solely from the Laravel adapter. Symfony spreads cache across many adapter classes with the read path living in a trait, so there's no single canonical seam to observe; the only single-class option is the dev-only `TraceableAdapter`, which is also extremely noisy (the framework hammers its system pools every request). It's deferred rather than captured half-complete. The Debug sub-tabs reflect this: a Symfony site shows Dumps, Queries, Mail, Views, Events, Jobs and HTTP; a Laravel site shows all of them.

## Queue workers (opt-in)

Long-running queue and scheduler workers (`queue:work`, `horizon`, `schedule:work`, `messenger:consume`) poll the database constantly, so capturing them by default would flood the in-memory buffer and bury the web-request queries you're actually debugging. Worker capture is therefore **off by default**: web requests and one-off CLI commands (artisan, tinker, migrations) are always captured, but worker processes are skipped unless you opt in.

Turn it on with the **Show worker queries** checkbox in the Debug window toolbar (present on every lens: Queries, Jobs, Views, Mail, Cache, Events, HTTP). Checking it arms worker capture by writing the `devtools-workers.flag` sentinel; from then on each worker invocation is captured and grouped on its own, labelled by the worker command, and a per-command filter dropdown appears so you can narrow to one worker. The Laravel adapter resets the request id on every `JobProcessing`, so each queued job is its own group rather than a worker's jobs lumping together.

Unchecking **Show worker queries** does two things: it stops capturing worker output going forward, and it immediately hides the worker rows already buffered in the view, so the lenses fall back to web and CLI activity without waiting for a buffer clear. The toggle is independent of the main Debug on/off switch.

## N+1 warnings

When a query shape repeats past a threshold (3×) within a single request or worker invocation, lerd fires one OS notification — **once per route/script per session** — so it warns you without nagging on every subsequent hit of the same endpoint. The dashboard also flags the request group with an **N+1** badge and tints the duplicate rows. Notifications respect the global `lerd notify` toggle.

## Debugging over MCP

The same capture is available to an AI assistant through lerd's MCP server, so an agent can debug and fix performance issues end to end. The loop: `dumps_toggle` to arm capture, `dumps_clear` for a clean slate, trigger the page or job, then `analyze_queries` for a per-request N+1 and slow-query report — each finding carries the originating `file:line`, so the agent can open the offending code and add a `with()` eager-load, an index, or a cache, then re-run to confirm the count dropped. `dumps_recent` with a `kind` filter (`query`, `mail`, `view`, …) pulls the raw events for anything the report doesn't cover. The analysis is server-side, so it uses the same fingerprinting as the dashboard badge and the N+1 notification.

## Open in editor

Every query's caller path in the Queries lens is a link. Expand a row to see the originating application frame (`Class::method — file:line`) and a **Details** button for the full stack trace; click any `file:line` to open it in your editor. lerd autodetects a known GUI editor (VS Code, Cursor, PhpStorm, Sublime, Zed, …); override it with an `editor` command in `~/.config/lerd/config.yaml`, e.g. `editor: "phpstorm --line {line} {file}"` ({file} and {line} are substituted). The endpoint is loopback-only.

## Caveats

- **PDO-backed databases.** Queries run on a PDO driver (including Doctrine over PDO) are captured; raw `mysqli` (WordPress) lands in follow-up work.
- **Bindings cover both styles.** Both `PDOStatement::execute([...])` arrays and individually bound `bindValue()` / `bindParam()` values are captured. Very large bound values (e.g. a serialized Messenger envelope) are shown verbatim.
- **Capture has overhead.** Like the debug bridge, leave it off when you're not actively debugging; it's a development tool, not something to run under load.
- **No persistence.** The buffer is in-memory and resets when `lerd-ui` restarts.
