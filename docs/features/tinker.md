# Tinker tab

Every PHP site in the lerd dashboard has a **Tinker** tab pinned to the bottom-right of the site header, next to **Overview**. It is an in-browser PHP REPL with autocomplete, live syntax checking, and an editor-like output panel: write code, hit Run, see the value of every statement instantly.

![Tinker tab](/assets/screenshots/site-detail-tinker.png)

Useful for the things you'd otherwise do in `php artisan tinker` or a `bin/console` session: one-off model lookups, data fixes, snippet experiments, regex sanity checks, expression evaluation against a real database.

## How a run works

The Run button POSTs your code to `lerd-ui`, which executes it inside the site's PHP container and returns the captured output. The execution mode is driven by the framework definition's `tinker:` block (see [framework definitions](#framework-defined-repl) below):

- **Framework-defined REPL** — when the active framework declares a `tinker:` block in its YAML and the declared `requires_package` / `requires_file` checks pass, lerd runs the framework's REPL. For Laravel that's `php artisan tinker --execute=...`, so the app is bootstrapped: `User::count()`, `Route::getRoutes()`, `Cache::get('foo')` all just work. The `mode` field in the response is set to the framework name (e.g. `"laravel"`).
- **Plain `php` fallback** — any site without a satisfied framework REPL (Symfony, vanilla PHP, Laravel without `laravel/tinker` installed). The code is written to a temp script inside the site, with `vendor/autoload.php` auto-required if it exists, and executed via `php {file}`. `mode` is `"php"`.

The active mode is shown as a small badge in the toolbar.

## Framework-defined REPL

Each framework YAML in `lerd-frameworks/frameworks/<name>/<version>.yaml` can declare a `tinker:` block:

```yaml
# laravel/12.yaml
tinker:
  command: ["artisan", "tinker"]   # appended to `php …` inside the container
  execute_flag: "--execute"        # how to pass user code (omit → pipe via stdin)
  requires_package: laravel/tinker # vendor/<this> must exist
  requires_file: artisan           # this path must exist relative to the site
```

Resolution rules:

1. If `requires_file` is set and the file is missing, the framework's REPL is skipped.
2. If `requires_package` is set and `vendor/<package>` is missing, the framework's REPL is skipped.
3. Otherwise, lerd runs `podman exec ... php <Command…>` and either appends `<execute_flag>=<code>` or pipes the code via stdin.

To add Tinker support for another framework, ship a `tinker:` block in its YAML — no Go changes needed. Examples:

```yaml
# A hypothetical Symfony with psysh installed
tinker:
  command: ["vendor/bin/psysh"]
  requires_package: psy/psysh
```

```yaml
# A Drupal-with-drush setup
tinker:
  command: ["vendor/bin/drush", "php-eval"]
  execute_flag: ""    # piped via stdin
  requires_file: drush.php
```

## Output rendering

The output panel is styled like a read-only editor: bordered box, monospace, line-number gutter rendered as CSS pseudo-elements so dragging across results never selects or copies the line numbers.

- **One block per top-level statement.** Backend injects an ASCII `0x1E` separator after each statement, frontend splits on it. Multi-line scripts produce a numbered list of outputs, not one concatenated blob.
- **Bare expressions auto-dump.** Type `User::count()` (no `dump`, no `echo`) and you see the value. The transformer wraps single-statement bare expressions in `dump(...)` (or `var_dump(...)` if Symfony VarDumper isn't installed). Statements that already produce side effects (`echo`, `return`, `throw`, control flow) are left alone.
- **Collapsible tree view for objects/arrays.** Symfony VarDumper output is parsed client-side into a tree: classes, arrays, scalars, with click-to-expand/collapse, color-coded scalars, and visibility prefixes (`+public`, `#protected`, `-private`).
- **Per-block Copy button** appears on hover.
- **Noise stripped server-side**: `[!] Aliasing 'X' to 'Y'` notices and `// vendor/psy/.../eval()'d code:N` annotations are removed before output is returned.

## Editor

The [Monaco editor](https://microsoft.github.io/monaco-editor/) (the engine behind VS Code) with PHP syntax highlighting, line numbers, bracket matching, undo/redo, and line wrapping. Light/dark theme follows the Lerd theme. Monaco is lazy-loaded the first time you open any editor surface, so it never weighs down the initial dashboard load.

### Language intelligence (phpantom_lsp)

Autocomplete, diagnostics, hover, and signature help are powered by [phpantom_lsp](https://github.com/PHPantom-dev/phpantom_lsp), a fast, self-contained Rust PHP language server. It bundles phpstorm-stubs and the Mago parser, so it needs no PHP runtime to analyze a project — lerd runs it on the host (managed binary in `~/.local/share/lerd/bin/phpantom_lsp`, like `fnm`/`mkcert`/`composer`) pointed at the site's project directory.

Because it analyzes the real project, completions are genuinely project-aware: your Eloquent models, relationships, scopes, casts and Builder chains resolve end-to-end, alongside framework facades, vendor classes, and the PHP standard library. Hover a symbol for its docblock; type `(` inside a call for signature help.

The browser connects to the server over a WebSocket (`/api/lsp/php`). `lerd-ui` spawns one `phpantom_lsp` process per connection, rooted at the site (or worktree) path, and bridges its stdio LSP traffic to Monaco. Tinker buffers are headerless PHP, so the bridge presents the document to the server with a synthetic leading `<?php` line (and offsets positions accordingly) — you keep typing bare snippets while the server still parses valid PHP.

A small status hint sits in the toolbar while the server is starting, and switches to "Language server unavailable" if it can't be reached. When that happens (offline first-run download, unsupported platform) the editor still works and code still runs — only the live intelligence is missing.

The server binary is fetched at `lerd install` time, and lazily on first connect for existing installs, so no manual setup is required.

### Keyboard

| Shortcut | Action |
|---|---|
| `Ctrl+Enter` / `Cmd+Enter` | Run the editor contents |
| `Ctrl+Space` | Trigger autocomplete |
| `Ctrl+Z` / `Ctrl+Y` | Undo / redo |

### Drafts

The editor contents are saved to `localStorage` under `tinker:{domain}:draft`, so refreshing the page or switching to another site and back doesn't lose what you typed. The active tab itself (`Overview` vs `Tinker`) also persists, under `lerd:siteDetailTab`.

## Toolbar

| Button | What it does |
|---|---|
| Mode badge | Shows `tinker` or `php`; tooltip explains which runtime is used. |
| Duration | Shown after a run, in milliseconds. |
| `Copy code` | Copies the editor contents to the clipboard. |
| `Clear` | Wipes both the editor and the output. |
| `Run` | Executes the code (also bound to Ctrl/Cmd+Enter). |

The output panel itself adds a per-block `Copy` button on hover, for copying just one of the numbered output blocks.

## When the tab is hidden

The Tinker tab is shown for any site that has a `php_version`. It is hidden for static-only sites and custom-container sites without a PHP runtime. Paused sites still get the tab — pausing only removes routing, the shared PHP-FPM container stays up.

## Limits

- Each run has a 30-second hard timeout (`exec.CommandContext`).
- Request body is capped at 64 KB.
- Each run is a fresh process. Variables don't persist across runs, this is not a stateful REPL session.
- Output is captured all at once, not streamed. A long-running script that prints incrementally only shows output after it finishes.
- ANSI colors are suppressed at the source (`NO_COLOR=1`, `TERM=dumb`) so output renders cleanly.

## Security note

The Tinker tab executes arbitrary PHP inside your site's container with the same access as the site itself: database, filesystem under the site path, every credential in `.env`. Treat it as equivalent to shell access to that container. `lerd-ui` only listens on `127.0.0.1:7073`, so this is bounded to the local machine, but any browser tab open to `http://lerd.localhost` can reach it.

## HTTP API

| Method + Path | Body | Returns |
|---|---|---|
| `POST /api/sites/{domain}/tinker` | `{ "code": "..." }` | `{ ok, stdout, stderr, exit_code, duration_ms, mode, error? }` |
| `GET /api/lsp/php?domain={domain}&branch={branch}` | WebSocket | LSP JSON-RPC bridged to `phpantom_lsp` (one message per text frame) |

Output from `tinker` runs uses ASCII `0x1E` (record separator) between top-level statements; the frontend splits on it. Aliasing notices and psysh source-location annotations are stripped before being returned.

The `/api/lsp/php` socket first sends a single `{"type":"lerd-root","root":"…"}` handshake frame so the browser can build the document URI for the workspace; every subsequent frame is a raw LSP JSON-RPC message.

## Implementation map

Backend (Go):

- `internal/cli/tinker.go` — `RunTinker`, the dump-function detector, the multi-statement transformer, `splitTopLevelStatements`, the auto-dump heuristic, output cleanup.
- `internal/phpantom/phpantom.go` — manages the `phpantom_lsp` host binary: platform asset resolution, pinned-version download, tar extraction into `BinDir`.
- `internal/ui/lsp.go` — `handleLSPPhp`, the WebSocket ↔ stdio LSP framing bridge (`readLSPMessage` / `encodeLSPMessage`).
- `internal/ui/server.go` — the `tinker` case on the site action handler and the `/api/lsp/php` route.
- Tests: `tinker_test.go`, `internal/ui/lsp_test.go`, `internal/phpantom/phpantom_test.go`.

Frontend (Svelte 5 + Monaco):

- `internal/ui/web/src/components/MonacoEditor.svelte` — reusable lazy-loaded Monaco wrapper.
- `internal/ui/web/src/lib/monaco.ts` — single-instance Monaco loader (editor.api + PHP grammar, themes, worker).
- `internal/ui/web/src/lib/lsp.ts` — dependency-free LSP client: completion/hover/signature providers, diagnostics-to-markers, headerless-REPL position mapping.
- `internal/ui/web/src/tabs/sites/SiteTinkerTab.svelte` — editor, LSP wiring, output rendering.
- `internal/ui/web/src/components/DumpView.svelte` — recursive collapsible tree.
- `internal/ui/web/src/lib/dump-parser.ts` — parses Symfony VarDumper CLI output into a tree (`+ tests`).
- `internal/ui/web/src/tabs/sites/SiteDetail.svelte` — host the tabs in the bottom-right of `SiteHeader`.
- `internal/ui/web/src/stores/sites.ts` — `runTinker` API helper.
