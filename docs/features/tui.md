# Terminal Dashboard (TUI)

`lerd tui` opens a btop-inspired full-screen dashboard in your terminal. It shows sites, services, workers, and logs in one glance, updates live, and lets you drive most of the same operations the web UI exposes without ever leaving the terminal.

```bash
lerd tui
```

This is the terminal-native counterpart to the [Web UI](/features/web-ui) and the [System Tray](/features/system-tray). Use it when you prefer to keep everything in a tmux or terminal pane, or when you're on a remote machine over SSH.

## Layout

- **Header** shows the lerd version, DNS / nginx / FPM status, watcher state, the wall clock, and an `update: vX.Y.Z` banner when a newer release is available (populated from the same 24-hour cache `lerd status` and `lerd doctor` use, so no extra network on startup). When any framework worker is failing a red `⚠ N` pill appears alongside the clock; press `H` to fire `lerd worker heal` and clear it.
- **Sites pane (left column, top)** lists every linked site by its primary domain, with an FPM running dot, PHP version, and worker glyphs (`q` queue, `s` schedule, `v` reverb, `h` horizon, plus a dot per custom framework worker). Paused sites are dimmed and marked. Columns line up across rows regardless of how many workers each site runs.
- **Services pane (left column, bottom)** is a compact list of built-in services (mysql, redis, postgres, meilisearch, rustfs, mailpit), custom services, and every site-owned worker (`queue-<site>`, `schedule-<site>`, `horizon-<site>`, `reverb-<site>`, and custom framework workers). Each row shows a running dot, how many sites use it, and `pinned` / `custom` tags where applicable.
- **Site detail (right column, full height)** always mirrors the focused site and shows primary domain, the Laravel `APP_NAME` when the site sets a custom one, internal name, disk path, all domains, services used (with live state), workers, git worktrees, HTTPS / LAN share toggles, and PHP / Node version pickers. `S` swaps it for global Settings, `?` swaps it for the Keybindings reference.
- **Logs pane** (toggle with `l`) tails the container, worker-journal, or app log file behind the focused item. Takes at least half the window and renders a right-edge scrollbar showing position in the buffer.
- **Status bar** briefly shows the most recent action (e.g. `✓ lerd service stop redis` or `✖ …exit 1`).
- **Footer** summarises active keybindings for the current mode.

Dots follow the same convention everywhere: green `●` running, grey `○` stopped, amber `◐` paused, red `✖` failing. A worker the idle engine has put to sleep reads `suspended` with an amber `◔` glyph, so a deliberately stopped-for-idle worker isn't mistaken for one that crashed or never started; it wakes on the next request.

## Keybindings

### Navigation

| Key | Action |
| --- | --- |
| `tab` / `shift+tab` | Cycle focus through Sites · Detail · Services so a tab from a freshly-selected site lands on its detail pane. Use `v` to hide Services from the cycle entirely |
| `↑` `↓` / `j` `k` | Move selection in the focused pane |
| `pgup` `pgdn` | Jump by 10 rows |
| `home` `g` | Jump to first row |
| `end` `G` | Jump to last row |

### Filter and sort

| Key | Action |
| --- | --- |
| `/` | Type to filter the focused list (matches name, domains, framework label) |
| `enter` | Commit filter and leave input mode |
| `esc` | Clear filter and leave input mode |
| `o` | Cycle sort order · **sites**: name → status → framework · **services**: name → status → usage (site count) |

### Actions

| Key | Action |
| --- | --- |
| `space` / `enter` | Toggle the focused detail row (worker, HTTPS, LAN share, PHP, Node) |
| `s` | Start / resume the focused site or start the focused service / worker |
| `x` | Stop / pause the focused site or stop the focused service / worker · on a domain row, remove that domain |
| `r` | Restart the focused site / service / worker |
| `p` | Pause / unpause toggle for a site |
| `t` | Open an interactive shell inside the focused container (FPM or custom for sites, the service container for services, the owning site's FPM for worker rows) |
| `O` | Open in the default browser (uses `xdg-open` on Linux, `open` on macOS): the focused site's primary domain, or — when the Services pane is focused — the focused service's dashboard URL (phpMyAdmin, Mailpit, RabbitMQ, RedisInsight, …). A service with no dashboard says so in the status bar |
| `u` | Run `lerd service update <name>` for the focused service so a presets bump or version pin lands without leaving the TUI. The action is in-strategy and reversible. |
| `b` | Run `lerd service rollback <name>` to swap the focused service back to its previous version; pairs with `u` as the symmetric undo |
| `H` | Run `lerd worker heal` to restart every failing framework worker in one pass. The header pill shows the count and the keybind is most relevant when it's lit |

### Logs

| Key | Action |
| --- | --- |
| `l` | Toggle the logs pane for the focused item |
| `[` / `]` | Cycle the log pane target through the site's log sources |
| `{` / `}` | Scroll back through buffered output / return to live tail |
| `f` | Find within the tailed buffer. Matches are highlighted, non-matching lines dim. Severity colouring (red for `ERROR / FATAL / PANIC / EXCEPTION / CRITICAL`, amber for `WARN / WARNING / DEPRECATED`) is always on |

### Domains

Available when focus is on the Detail pane with the cursor on a domain row.

| Key | Action |
| --- | --- |
| `a` | Add a new domain to the focused site (opens inline input) |
| `e` | Edit / rename the focused domain (opens inline input prefilled with the short name; commit runs `lerd domain add <new>` then `lerd domain remove <old>` as a sequence) |
| `x` | Remove the focused domain |

### Panes and overlays

| Key | Action |
| --- | --- |
| `v` | Show / hide the Services pane |
| `F` | Swap the Detail pane for the Dashboard (counts, system health, container resources) and focus it |
| `S` | Swap the Detail pane for global Settings (LAN expose, autostart, Xdebug) and focus it |
| `Y` | Swap the Detail pane for the System overview (DNS, Nginx, Watcher, Notifications, Debug bridge, PHP per-version, Node, Lerd) and focus it |
| `D` | Open the Debug window, the same capture the web dashboard shows. `[` / `]` switch lens across `Dumps · Queries · Jobs · Views · Mail · Cache · Events · HTTP`; the Queries lens groups by request with N+1 and slow-query (≥100ms) flags, and the other lenses group by request too. Use `/` to search the active lens (site, request, worker, file, text, payload) · `1`/`2` toggle the FPM / CLI context-filter chips · `enter` expands the selected row (query bindings and caller, job exception, view template, mail recipients, …) · `w` toggles worker capture (queue / scheduler events, off by default) · `c` clears the buffer (and runs `lerd dump clear`) · `T` toggles the bridge globally. The buffer is independent of the lerd-ui ring because the TUI runs in its own process and only sees what the SSE connection delivers |
| `?` | Open the Keybindings reference as a centered modal overlay; `?` again or `esc` closes it |
| `esc` | Dismiss the active modal (palette / picker / help / confirm), return to the pane underneath |

### General

| Key | Action |
| --- | --- |
| `:` | Open the command palette — type any `lerd <args>` (e.g. `service restart redis`) and press enter to shell out exactly as if you'd typed it in a regular terminal |
| `R` | Force a state refresh |
| `q` / `ctrl+c` | Quit |

## Log sources

When the log pane is open, `[` and `]` cycle through every tail-able source for whatever's focused:

- **FPM / custom container** — `podman logs -f lerd-php<ver>-fpm` for PHP sites, or `lerd-custom-<name>` for custom container sites.
- **Workers** — `journalctl --user -u lerd-queue-<site>` (and the same for schedule, reverb, horizon, custom framework workers). Workers are systemd user units, not containers, so their output lives in the user journal.
- **App logs** — any file matching the framework's declared log globs (Laravel: `storage/logs/*.log`). Tailed with `tail -F` so rotated Laravel-style logs keep following.

The pane title shows which source is active and the index, e.g. `Logs · astrolov · laravel.log [3/5 · [ ] to switch]`.

## Service detail

When focus is on the Services pane, the right column swaps to a service-focused detail mirroring the web UI's `ServiceDetail`. Sections, top to bottom:

- **Header** — service name, version, state, systemd unit, pinned flag, and the dashboard URL (when the preset declares one); press `O` to open that URL in the browser.
- **Depends on** — services in `depends_on`, each with its live state so you can confirm a stack is fully up before debugging.
- **Sites using** — every active site (excluding paused/ignored) whose `.lerd.yaml` references this service.
- **Env vars** — the preset's `env_vars` template list for default presets, or the merged `env_vars` + `environment` map for custom services. Read-only.
- **Preset suggestion** — a one-line nudge for the matching admin dashboard preset (e.g. `mysql` → install `phpmyadmin`) when it isn't already on disk. Install is destructive enough to stay CLI-only per the TUI scope rule, so the banner points at `lerd preset install <name>` rather than wiring an in-TUI installer.
- **Actions** — quick reminder of the reversible verbs the services pane already handles: `s start`, `x stop`, `r restart`, `t shell`, `u update`, `b rollback`, `l logs`.

For worker rows (queue-X, schedule-X, custom framework workers) the detail variant skips the env / dependency / sites-using sections and just shows the worker kind, the parent site, the systemd user unit, and the project path — workers run inside the owning site's FPM container, so they have no env or image of their own.

## Site detail tabs

The site detail pane is split into four read-side tabs the user can jump between with the number keys, mirroring the web UI's `Overview / Env / Tinker / Debug` strip (Tinker is CLI-only since it needs an interactive REPL):

| Key | Tab | Contents |
| --- | --- | --- |
| `1` | Overview | The default — domains, services used, workers, worktrees, toggles (HTTPS / LAN / PHP / Node) |
| `2` | Env | Read-only display of the site's `.env` file (read up to 256 KB so a runaway file can't wedge the render loop) |
| `3` | Debug | This site's slice of the Debug window: the active lens (Dumps · Queries · Jobs · Views · Mail · Cache · Events · HTTP) scoped to the focused site, with `[` / `]` to switch lens and `w` to toggle worker capture. Rows show their detail inline; press `D` for the full cross-site window |
| `4` | App logs | Every framework-declared log file with size and modification time; press `l` to actually tail one — the file targets are wired into `logTargetsForSite`, so `[` / `]` cycle through them once the log pane is open |
| `5` | Doctor | Laravel only — the same app-level health checks the web dashboard runs (`APP_KEY`, `.env` drift against `.env.example` warning only on keys the code reads without a default, the `APP_DEBUG`-in-production footgun, the `public/storage` symlink, and pending migrations). The migrations check execs artisan in the container, so the run is on-demand: the tab appears only for Laravel sites, press `5` to run and again to re-run. The panel is read-only and names the suggested fix (e.g. `key:generate`, `migrate`) rather than running it, so a status view can never migrate a database |

Switching tabs resets the detail-pane scroll so the user lands at the top of the new tab. Picker overlays (PHP / Node version) only show in Overview; selecting a different tab dismisses them.

## Site detail

The detail pane is the main control surface for a site. With focus on the Sites pane, moving the cursor updates the detail live. Press `tab` until focus lands on the Detail pane to navigate its rows and toggle them with `space`.

Sections, top to bottom:

- **Header** — primary domain (the URL users visit), internal name, disk path.
- **Domains** — every domain on one row, each tagged `primary · e edit · x remove` or `alias · e edit · x remove`. Ends with `+ add domain (space or a)` to insert new ones.
- **PHP / Node / framework / git branch** — one-line summary.
- **Services used** — every service referenced in `.lerd.yaml` with its live state, so you can see at a glance whether redis / mysql / etc. are up for this site.
- **Workers** — queue, schedule, horizon, reverb, and any custom framework workers, each with a running / failing indicator. `space` on a worker row toggles it (calls `lerd queue start/stop`, etc.).
- **Worktrees** — every git worktree with its branch, domain, and path when the site uses them. Each worktree row carries its own controls — PHP / Node version pickers, LAN-share toggle, isolated-DB toggle, and per-worktree framework worker toggles (e.g. vite) — so a branch's runtime can be tuned without affecting the parent. `space` on a worktree-scoped row toggles the matching state via the same CLI commands the parent rows use, just with the worktree's path threaded through.
- **Toggles** — HTTPS (runs `lerd secure` / `lerd unsecure`), LAN share (runs `lerd lan share` / `unshare` — shows the full `http://<lan-ip>:<port>` URL when enabled), PHP version (opens an inline picker from installed versions → `lerd isolate <ver>`; a FrankenPHP site only lists the versions FrankenPHP publishes an image for, so the picker never offers one that would silently downgrade), Node version (picker backed by `fnm list` → `lerd isolate:node <ver>`; when a host bun is installed the list also carries a `bun` entry that pins the site's JS runtime via `lerd js:runtime bun`, and picking a Node version while pinned to bun clears the pin first so the dev worker actually switches back).

## Settings view

Press `S` to swap the detail pane for global settings. Navigate with `↑` `↓`, toggle with `space`:

- **LAN expose** — flip every container to 0.0.0.0 binds (`lerd lan expose on/off`).
- **Autostart on login** — `lerd autostart enable/disable`.
- **Xdebug** — one toggle per installed PHP version; rebuilds the FPM container.

`S` again (or `esc`) returns to Site detail.

## Dashboard view

Press `F` to swap the detail pane for the Dashboard — the terminal counterpart to the web UI's home page. It is purely informational; every reversible action lives in the System pane (`Y`) instead. Sections:

- **Hero** — failing-worker count (or an "all workers healthy" confirmation), plus an update banner when a newer lerd release is available, or the current version if not.
- **Overview** — `Sites` (total · running · paused), `Services` (total · running · stopped), `Workers` (active · failing with a `press H` hint when any are red).
- **System health** — DNS (mirrors the header pill: ok / degraded / down / disabled), Nginx, Watcher, and the comma-separated list of running PHP FPM versions.
- **Resources** — total CPU% and total memory across lerd's whole footprint: the `lerd-*` containers (via `podman stats`) plus lerd's own host-side processes (the lerd-ui/watcher/tray daemons and any host worker such as a Vite dev server, via systemd cgroup accounting on Linux). The top five are ranked by combined CPU+memory load, so a CPU-heavy but low-memory process surfaces rather than being hidden behind idle memory hogs. CPU is the instantaneous rate (lerd reads past `podman stats`'s first sample, which is a misleading lifetime average). Polled in the background every 3 s, matching the cache TTL the web UI uses so the two surfaces stay aligned. While the first sample is in flight a `collecting…` placeholder renders so an empty pane is never shown.
- **Lerd** — version, autostart, LAN expose, and platform (`linux/amd64`, `darwin/arm64`, …).

`F` again or `esc` returns to Site detail.

## System view

Press `Y` to swap the detail pane for the System overview — the terminal-side counterpart to the web UI's System tab. Sections cover every shared subject lerd manages outside of an individual site, with informational rows for status and reversible toggles for safe operations:

- **DNS** — TLD, live status (ok · degraded · down · disabled) computed by `dns.CheckStatus`, plus a VPN-active hint when an interface that typically rewrites the system resolver is up.
- **Nginx** — running / stopped.
- **Watcher** — running / stopped.
- **Notifications** — `Enabled` toggle (runs `lerd notify on/off`).
- **Debug bridge** — `Enabled` toggle (runs `lerd dump on/off`), passthrough indicator (web-UI managed), listen socket address, and the current TUI buffered count.
- **PHP versions** — default version plus one row per installed PHP showing FPM running state and an Xdebug toggle that reflects the configured mode (`debug`, `profile`, or `trace`).
- **Node** — default version (from the global config) and the installed major versions reported by `fnm list`.
- **Worker mode** — macOS only; toggles `lerd workers mode exec|container`. Hidden on Linux where workers always run under systemd.
- **Lerd** — current version, cached update check result, autostart toggle, LAN-expose toggle.

Navigate the rows with `↑` `↓` (the cursor skips section headers and info-only rows), `space` / `enter` to toggle. `Y` again or `esc` returns to Site detail. Every toggle shells out to the public CLI verb so the TUI shares the same code path as a manual `lerd …` invocation.

## Keybindings reference

Press `?` to open the full keybinding reference as a centered modal overlay. Scroll with `↑` `↓`, `pgup` / `pgdn`, or `g` / `home` to jump to the top. `?` again or `esc` closes it. `q` still quits even while the overlay is open.

## Toasts

Action results (a service restart, a worker heal, a toggle) land as **toast notifications** in the bottom-right corner: a coloured severity dot (green / amber / red), bold title (the CLI invocation), and a dim subtext for any error message. Up to three toasts stack vertically; older ones drop after 30 s. Press `d` to dismiss the newest manually. Identical back-to-back toasts coalesce so a busy moment doesn't bury the screen.

During an in-flight action the status line (just above the toasts) shows an animated Braille spinner (`⠋⠙⠹…`) so the user feels the action is alive even when the underlying CLI takes a few seconds.

## Modal overlays

A handful of focused surfaces render as centered modal overlays (rounded border, accent colour) rather than swapping the detail pane:

- **Command palette** (`:`) — `lerd <args>` prompt with tab-completion suggestions; runs the command in a suspended shell so the output is visible, then pauses for `enter` before returning to the dashboard.
- **PHP / Node version picker** — opens when `space` / `enter` lands on the PHP or Node row (site- or worktree-scoped). Pick with `↑` / `↓`, apply with `enter`, dismiss with `esc`.
- **Keybindings reference** (`?`) — described above.
- **Confirmation prompt** — guards destructive single-key actions (e.g. `x` on a domain row). `y` confirms, `n` / `esc` cancels.

While any modal is open it owns every keystroke; `esc` returns to whatever pane was focused underneath.

## Live updates

The TUI draws state from the same sources `lerd-ui` uses, in-process:

- Subscribes to the shared eventbus so any mutation the TUI itself triggers shows up immediately (150 ms debounce).
- Re-queries every 2 seconds as a safety net, so changes made from another terminal (`lerd service stop redis` in a different shell) surface within a couple of seconds.
- Services and site state are built from the same `siteinfo` + `podman.Cache` path the web UI uses, so the two surfaces can't disagree.

## Troubleshooting

- **Terminal too small** — if the window is under 60 columns by 12 rows the dashboard refuses to render and asks you to resize. It picks up the new size on the next frame.
- **Non-interactive shells** — `lerd tui` exits with an error when stdout isn't a TTY (piped output, CI). Run it inside a real terminal.
- **Worker log says nothing** — check the worker is actually running (`lerd status` or the Workers section of the detail pane). Journal logs only exist while the unit has run at least once.
