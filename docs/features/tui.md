# Terminal Dashboard (TUI)

`lerd tui` opens a btop-inspired full-screen dashboard in your terminal. It shows sites, services, workers, and logs in one glance, updates live, and lets you drive most of the same operations the web UI exposes without ever leaving the terminal.

```bash
lerd tui
```

This is the terminal-native counterpart to the [Web UI](/features/web-ui) and the [System Tray](/features/system-tray). Use it when you prefer to keep everything in a tmux or terminal pane, or when you're on a remote machine over SSH.

## Layout

- **Header** shows the lerd version, DNS / nginx / FPM status, watcher state, the wall clock, and an `update: vX.Y.Z` banner when a newer release is available (populated from the same 24-hour cache `lerd status` and `lerd doctor` use, so no extra network on startup).
- **Sites pane (left column, top)** lists every linked site by its primary domain, with an FPM running dot, PHP version, and worker glyphs (`q` queue, `s` schedule, `v` reverb, `h` horizon, plus a dot per custom framework worker). Paused sites are dimmed and marked. Columns line up across rows regardless of how many workers each site runs.
- **Services pane (left column, bottom)** is a compact list of built-in services (mysql, redis, postgres, meilisearch, rustfs, mailpit), custom services, and every site-owned worker (`queue-<site>`, `schedule-<site>`, `horizon-<site>`, `reverb-<site>`, and custom framework workers). Each row shows a running dot, how many sites use it, and `pinned` / `custom` tags where applicable.
- **Site detail (right column, full height)** always mirrors the focused site and shows primary domain, internal name, disk path, all domains, services used (with live state), workers, git worktrees, HTTPS / LAN share toggles, and PHP / Node version pickers. `S` swaps it for global Settings, `?` swaps it for the Keybindings reference.
- **Logs pane** (toggle with `l`) tails the container, worker-journal, or app log file behind the focused item. Takes at least half the window and renders a right-edge scrollbar showing position in the buffer.
- **Status bar** briefly shows the most recent action (e.g. `✓ lerd service stop redis` or `✖ …exit 1`).
- **Footer** summarises active keybindings for the current mode.

Dots follow the same convention everywhere: green `●` running, grey `○` stopped, amber `◐` paused, red `✖` failing.

## Keybindings

### Navigation

| Key | Action |
| --- | --- |
| `tab` / `shift+tab` | Cycle focus through Sites · Services · Detail |
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

### Logs

| Key | Action |
| --- | --- |
| `l` | Toggle the logs pane for the focused item |
| `[` / `]` | Cycle the log pane target through the site's log sources |

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
| `S` | Swap the Detail pane for global Settings (LAN expose, autostart, Xdebug) and focus it |
| `?` | Swap the Detail pane for this Keybindings reference |
| `esc` | Close picker, return to Site detail |

### General

| Key | Action |
| --- | --- |
| `R` | Force a state refresh |
| `q` / `ctrl+c` | Quit |

## Log sources

When the log pane is open, `[` and `]` cycle through every tail-able source for whatever's focused:

- **FPM / custom container** — `podman logs -f lerd-php<ver>-fpm` for PHP sites, or `lerd-custom-<name>` for custom container sites.
- **Workers** — `journalctl --user -u lerd-queue-<site>` (and the same for schedule, reverb, horizon, custom framework workers). Workers are systemd user units, not containers, so their output lives in the user journal.
- **App logs** — any file matching the framework's declared log globs (Laravel: `storage/logs/*.log`). Tailed with `tail -F` so rotated Laravel-style logs keep following.

The pane title shows which source is active and the index, e.g. `Logs · astrolov · laravel.log [3/5 · [ ] to switch]`.

## Site detail

The detail pane is the main control surface for a site. With focus on the Sites pane, moving the cursor updates the detail live. Press `tab` until focus lands on the Detail pane to navigate its rows and toggle them with `space`.

Sections, top to bottom:

- **Header** — primary domain (the URL users visit), internal name, disk path.
- **Domains** — every domain on one row, each tagged `primary · e edit · x remove` or `alias · e edit · x remove`. Ends with `+ add domain (space or a)` to insert new ones.
- **PHP / Node / framework / git branch** — one-line summary.
- **Services used** — every service referenced in `.lerd.yaml` with its live state, so you can see at a glance whether redis / mysql / etc. are up for this site.
- **Workers** — queue, schedule, horizon, reverb, and any custom framework workers, each with a running / failing indicator. `space` on a worker row toggles it (calls `lerd queue start/stop`, etc.).
- **Worktrees** — every git worktree with its branch, domain, and path when the site uses them.
- **Toggles** — HTTPS (runs `lerd secure` / `lerd unsecure`), LAN share (runs `lerd lan share` / `unshare` — shows the full `http://<lan-ip>:<port>` URL when enabled), PHP version (opens an inline picker from installed versions → `lerd isolate <ver>`), Node version (picker backed by `fnm list` → `lerd isolate:node <ver>`).

## Settings view

Press `S` to swap the detail pane for global settings. Navigate with `↑` `↓`, toggle with `space`:

- **LAN expose** — flip every container to 0.0.0.0 binds (`lerd lan expose on/off`).
- **Autostart on login** — `lerd autostart enable/disable`.
- **Xdebug** — one toggle per installed PHP version; rebuilds the FPM container.

`S` again (or `esc`) returns to Site detail.

## Keybindings reference

Press `?` to swap the detail pane for the full keybinding reference, scrollable with `↑` `↓`. `?` again (or `esc`) returns to Site detail.

## Live updates

The TUI draws state from the same sources `lerd-ui` uses, in-process:

- Subscribes to the shared eventbus so any mutation the TUI itself triggers shows up immediately (150 ms debounce).
- Re-queries every 2 seconds as a safety net, so changes made from another terminal (`lerd service stop redis` in a different shell) surface within a couple of seconds.
- Services and site state are built from the same `siteinfo` + `podman.Cache` path the web UI uses, so the two surfaces can't disagree.

## Troubleshooting

- **Terminal too small** — if the window is under 60 columns by 12 rows the dashboard refuses to render and asks you to resize. It picks up the new size on the next frame.
- **Non-interactive shells** — `lerd tui` exits with an error when stdout isn't a TTY (piped output, CI). Run it inside a real terminal.
- **Worker log says nothing** — check the worker is actually running (`lerd status` or the Workers section of the detail pane). Journal logs only exist while the unit has run at least once.
