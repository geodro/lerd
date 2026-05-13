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

The output panel is styled like a read-only CodeMirror: bordered box, monospace, line-number gutter rendered as CSS pseudo-elements so dragging across results never selects or copies the line numbers.

- **One block per top-level statement.** Backend injects an ASCII `0x1E` separator after each statement, frontend splits on it. Multi-line scripts produce a numbered list of outputs, not one concatenated blob.
- **Bare expressions auto-dump.** Type `User::count()` (no `dump`, no `echo`) and you see the value. The transformer wraps single-statement bare expressions in `dump(...)` (or `var_dump(...)` if Symfony VarDumper isn't installed). Statements that already produce side effects (`echo`, `return`, `throw`, control flow) are left alone.
- **Collapsible tree view for objects/arrays.** Symfony VarDumper output is parsed client-side into a tree: classes, arrays, scalars, with click-to-expand/collapse, color-coded scalars, and visibility prefixes (`+public`, `#protected`, `-private`).
- **Per-block Copy button** appears on hover.
- **Noise stripped server-side**: `[!] Aliasing 'X' to 'Y'` notices and `// vendor/psy/.../eval()'d code:N` annotations are removed before output is returned.

## Editor

CodeMirror 6 with PHP syntax highlighting, line numbers, bracket matching, undo/redo, line wrapping. Light/dark theme follows the Lerd theme.

### Autocomplete

Tab opens the popup; if it's already open, Tab accepts the highlighted entry. Tab never escapes focus from the editor (so you can keep typing without losing your place).

What's offered, in priority order:

1. **Project models** (boost 10) — classes that look like Eloquent models, Doctrine ORM entities, Pivot, MorphPivot, or `extends Authenticatable`.
2. **Project classes** (boost 5) — every other class declared in your PSR-4 autoload roots, read from `composer.json`'s `autoload.psr-4` and `autoload-dev.psr-4`. Works for Laravel `app/`, Symfony `src/`, or any custom mapping.
3. **Composer-loaded global functions** — extracted from `vendor/composer/autoload_files.php`. Picks up Laravel's `collect()`, `dd()`, `dump()`, `tap()`, plus any Symfony / package helpers registered via composer's `files` autoload.
4. **PHP internal functions** — `get_defined_functions(true)['internal']` for the site's PHP version, ~2,200 entries. Cached per version, so the first symbol fetch pays a ~80 ms PHP exec, subsequent ones are instant.
5. **Framework hints** — Laravel facades + helpers when `is_laravel`, Symfony framework classes (`Request`, `Response`, `EntityManagerInterface`, `AbstractController`, `Form`, `Command`, …) when `framework === 'symfony'`.
6. **PHP standard library** — common classes (`DateTime`, `PDO`, `ReflectionClass`, `Closure`, `Generator`, `Stringable`) and functions.
7. **Buffer variables** — typing `$u` after declaring `$user = ...` suggests `$user`. Source scans the editor for `$varname` tokens.
8. **Any-word fallback** — words seen anywhere in the buffer.

Context-aware sources kick in for two patterns:

- After `Model::` — Eloquent **static** methods (`find`, `where`, `paginate`, `firstOrCreate`, `count`, …).
- After `->` — Eloquent / Builder / Collection **instance** methods (`save`, `update`, `pluck`, `each`, `map`, …).

Each entry shows a colored type icon and an uppercase detail label on the right (`MODEL`, `CLASS`, `FACADE`, `HELPER`, `METHOD`, `STATIC`, `FUNCTION`, `PHP FN`, `PHP CLASS`, `SYMFONY`, `VAR`).

### Live syntax checking

Edits trigger `php -l` against the site's PHP container, debounced 600 ms. Results are rendered as inline CodeMirror diagnostics:

- Parse / fatal errors → red gutter dot, red wavy underline, hover tooltip with the message.
- Warnings / deprecations / notices → amber, same UI.

The PHP version used is the site's own (8.4 features in an 8.4 site won't trip the linter).

### Keyboard

| Shortcut | Action |
|---|---|
| `Ctrl+Enter` / `Cmd+Enter` | Run the editor contents |
| `Tab` | Open autocomplete (or accept selected entry) |
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
| `POST /api/sites/{domain}/tinker:symbols` | (none) | `{ models: [...], classes: [...], functions: [...] }` |
| `POST /api/sites/{domain}/tinker:lint` | `{ "code": "..." }` | `{ ok, diagnostics: [{ line, column, message, severity }], error? }` |

Output from `tinker` runs uses ASCII `0x1E` (record separator) between top-level statements; the frontend splits on it. Aliasing notices and psysh source-location annotations are stripped before being returned.

## Implementation map

Backend (Go):

- `internal/cli/tinker.go` — `RunTinker`, the dump-function detector, the multi-statement transformer, `splitTopLevelStatements`, the auto-dump heuristic, output cleanup.
- `internal/cli/tinker_symbols.go` — `CollectTinkerSymbols`, PSR-4 autoload root resolution, composer autoload-files function harvesting, cached `get_defined_functions()` exec.
- `internal/cli/tinker_lint.go` — `LintTinkerCode` runs `php -l` and parses the output to diagnostics.
- `internal/ui/server.go` — `tinker`, `tinker:symbols`, `tinker:lint` cases on the site action handler.
- Tests: `tinker_test.go`, `tinker_symbols_test.go`, `tinker_lint_test.go`.

Frontend (Svelte 5 + CodeMirror 6):

- `internal/ui/web/src/tabs/sites/SiteTinkerTab.svelte` — editor, autocomplete sources, linter integration, output rendering.
- `internal/ui/web/src/components/DumpView.svelte` — recursive collapsible tree.
- `internal/ui/web/src/lib/dump-parser.ts` — parses Symfony VarDumper CLI output into a tree (`+ tests`).
- `internal/ui/web/src/tabs/sites/SiteDetail.svelte` — host the tabs in the bottom-right of `SiteHeader`.
- `internal/ui/web/src/stores/sites.ts` — `runTinker`, `lintTinker`, `loadTinkerSymbols` API helpers.
