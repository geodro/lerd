# Web UI

Lerd includes a browser dashboard available at **`http://lerd.localhost`**, served by the `lerd-ui` systemd service (started automatically with `lerd install`).

```bash
lerd dashboard   # open in your default browser
```

The `.localhost` TLD resolves to `127.0.0.1` natively on all modern systems, no DNS configuration needed. The dashboard is also reachable directly at `http://127.0.0.1:7073` if nginx is not running.

## Real-time updates

The dashboard opens a single WebSocket to `/api/ws` on load and receives state changes as they happen. No polling, no stale panels. Every surface that mutates lerd state (browser actions, `lerd` CLI commands, the MCP server, the file watcher) pushes a fresh snapshot to every connected tab within about 200 ms. If the WebSocket ever drops (e.g. `lerd-ui` restart), the dashboard falls back to a 5 s polling loop and reconnects in the background with exponential backoff, so a restart is transparent.

Behind the scenes a background container poll runs every 15 s when at least one tab is visible and the desktop session is active, and drops to 60 s otherwise (every tab hidden, or the session reported idle or locked by systemd-logind). Battery-aware: a focused tab on a locked laptop still falls back to the slow cadence.

## Install as an app

The dashboard is a Progressive Web App (PWA). You can install it as a standalone desktop app from any Chromium-based browser (Chrome, Brave, Edge):

1. Open `http://lerd.localhost`
2. Click the **install** icon (⊕) in the address bar
3. Click **Install**

Once installed, Lerd opens in its own window without browser chrome, just like a native app.

A service worker ships with the dashboard, so when lerd is stopped (including via `lerd quit`) or restarting, the installed PWA shows a small offline landing page instead of the usual browser "this site can't be reached" error. The page surfaces `lerd start` as the restart command and auto-reloads the dashboard as soon as the backend comes back up.

---

## Layout

The default landing page is a **Dashboard** with at-a-glance widgets across sites, services, workers, and system health. Selecting Sites, Services, or System switches to a three-pane layout:

- **Left icon rail**: the lerd logo at the top is the link back to the Dashboard; below it sit the Sites, Services, and System icon buttons; a separator further down lists a per-service icon for every running service that exposes a dashboard (phpMyAdmin, pgAdmin, Mailpit, RustFS, Meilisearch, Mongo Express, Selenium, etc.), and clicking one opens that dashboard inline as a full-width iframe over the middle and detail panels; theme toggle and docs link at the bottom
- **Middle list panel**: scrollable list of all items in the active section; status dots, compact rows, collapsible groups (hidden on the Dashboard)
- **Detail panel**: full controls and live logs for the selected item

On mobile the dashboard, list, and detail panels are full-screen with a bottom tab bar for navigation that includes Dashboard alongside Sites, Services, and System.

### Language

The dashboard ships in seven languages: English, German, Spanish, French, Indonesian, Dutch, and Portuguese. The first time you open the dashboard, the language is autodetected from your browser's preferred locale (Paraglide reads `navigator.language` and picks the closest match, falling back to English). To change it later, open **System → Lerd** and use the language picker in the settings section; the choice persists in `localStorage` so subsequent sessions open in the same language. Strings are wired through Paraglide; new keys fall back to English when a locale hasn't been translated yet.

![System → Lerd settings with the language picker](/assets/screenshots/system-lerd.png)

---

## Dashboard

![Dashboard root page](/assets/screenshots/dashboard.png)

The Dashboard is the root page (`#dashboard`) and the default destination when the UI loads. It hides the middle list panel and fills the main pane with a responsive grid of widgets:

- **Sites**: total / running / paused / failing counts, the top frameworks across linked sites as red badges, and a **Link site** call to action (loopback only) that opens the same modal as the Sites tab `+` button.
- **Services**: an active-vs-total summary pill, a click-through banner when one or more services have updates available, a two-column list of every core service with status dot and version, an **Add** button (loopback only) that opens the preset picker, and a link into the Services tab.
- **Workers**: per-group counts (Queues, Schedules, Horizon, Reverb, Stripe, custom Workers), a red pulsing dot when any unit in a group is failing, and a **Heal all** button that runs the same heal flow as the worker-health banner. Otherwise shows an "All healthy" pill.
- **System health**: overall pill (Healthy / Attention / Problem) derived from DNS, Nginx, and the file watcher, plus a row per component and a chip per installed PHP-FPM version coloured by its running state.
- **Lerd**: current version, "Up to date" or a yellow "update available" banner with an **Open terminal & update** button (loopback only), Autostart and LAN status pills, plus **Check for updates** and **Manage →** in the footer.

Every widget is driven by the same Svelte stores that power the rest of the dashboard, so all values stay live over the WebSocket without polling.

### Command palette

![Command palette overlay](/assets/screenshots/command-palette.png)

Press **`Cmd+K`** (macOS) / **`Ctrl+K`** (Linux/Windows), or **`/`** anywhere outside an input, to open a global command palette overlay. It searches across:

- **Pages** — Dashboard, Sites, Services, System
- **Sites** — every linked domain, with framework hint
- **Services** — every core service, with version hint
- **Actions** — Link a site, Add a service, Heal failing workers (when any), Check for updates, Open documentation, Open current site in browser, Toggle theme

Use `↑` / `↓` to move the selection, `↵` to execute, `esc` to close. The palette is available on every tab, not just the dashboard.

---

## Sites

![Sites tab](/assets/screenshots/sites-list.png)

The middle panel lists all registered projects. Active sites show a status dot (green when FPM is running), domain name, and small indicator dots for running workers (amber for queue/horizon, sky for reverb, emerald for schedule, violet for custom workers). Paused sites appear in a separate collapsible section.

A **sort button** floats in the bottom-right corner of the list; clicking it opens a small menu with **Most used** (sites with the most recent app-log activity float to the top), **Alphabetical** (by domain), and **Newest first** (reverses the order sites were linked in). The choice is remembered across reloads. A drag handle appears on a site row when you hover it; grab it to drag the row into a new position, with the dragged row and the rest of the list animating smoothly into place. Dragging switches the list into a manual order seeded from whatever you were looking at, so there's no separate "manual" mode to pick. The new order is saved to `sites.yaml` and pushed live to any other open tab. Grouped subdomains always travel with their main site, and the paused section stays at the bottom.

![Site detail with the Overview tab open](/assets/screenshots/site-detail-overview.png)

Selecting a site opens the detail panel with:

- **Address bar header**: a browser-style row with the site's favicon, scheme, and domain. The leading **lock icon** toggles TLS in one click (green closed when enabled, gray open when disabled, static on worktrees and when DNS is off). Clicking the URL text opens the Manage Domains modal. The trailing actions are a circular **reload** (restart FPM), **pause/resume**, **open in browser**, a **wifi** toggle for LAN sharing, **terminal** (loopback only), and **unlink**. On narrow panels the secondary actions fold into a ⋮ menu. When LAN sharing is on, the shareable URL appears as a teal chip inside the address bar with a hover-QR.
- **PHP / Node dropdowns**: change the version per site; writes `.php-version` / `.node-version` into the project and regenerates the nginx vhost on the fly
- **Queue toggle**: start or stop the queue worker; amber when running; live log stream below
- **Schedule toggle**: start or stop the task scheduler; live log stream below
- **Reverb toggle**: start or stop the Reverb WebSocket server; only shown when the project uses Reverb (detected via composer or `.env`)
- **Framework worker toggles**: additional workers defined by the site's framework (e.g. Symfony `messenger`, Laravel `horizon`) appear as indigo toggles
- **Stripe toggle**: start or stop the Stripe webhook listener
- **Pause / Resume**: suspend a site's nginx vhost without unlinking it; the site stays registered and FPM keeps running. When a paused site is selected, the detail pane hides the overview/tinker/dumps tabs and shows a centered Resume placeholder so it's obvious the site is offline on purpose rather than broken

  ![Paused site detail with the Resume placeholder](/assets/screenshots/site-detail-paused.png)

- **Link site**: a CTA on the dashboard Sites widget and a **+** button next to the Sites list header open the Link Site modal, which lets you pick a directory below your home folder. Submitting it runs `lerd link <path>` and the new site shows up immediately.

  ![Link Site modal](/assets/screenshots/link-site-modal.png)

- **Unlink button**: remove a site from nginx without touching the terminal
- **Git Worktrees**: on a git-backed site the address bar grows a tab strip above it. The first tab is the main checkout (home icon), followed by one tab per worktree (git-branch icon, branch name); the active worktree's path is shown on the right of the strip. Each worktree tab has an inline **×** that opens the Remove Worktree modal scoped to that branch (force / drop-db checkboxes). A trailing **+** opens the Add Worktree modal. Each worktree's framework-worker toggles still appear in the controls row below the address bar, so a per-worktree Vite dev server, queue, or schedule worker can be flipped on without affecting the parent. Worktree workers run under `lerd-<wname>-<site>-<wt>` units; the Workers group on the Services tab groups them under the same label as the parent site (e.g. "Vite") with a `branch` chip per row
- **Add Worktree modal**: asks for a new branch or existing branch, the database choice (share parent / isolated empty / clone main / clone another worktree / reuse a preserved isolated DB), an optional Run migrations checkbox, and the frontend-asset build choice (Automatic / asset worker / npm script / skip). Submission streams the underlying `lerd worktree add` output live in the modal, including the `Automatic: ...` resolution line so the picked build path is never silent. The modal closes on success, or stays open with a warnings banner if any `[WARN]` lines were emitted
- **Remove Worktree modal**: opens scoped to a single branch when its tab's × is clicked. Offers a *Discard uncommitted changes* (force) checkbox and, when isolated, an *Also drop database* checkbox. Runs `lerd worktree remove` and closes once the branch is gone
- **Live PHP-FPM log**: streams FPM output for the selected site; tab switches to queue/horizon/schedule/reverb logs when those workers are running

  ![Live PHP-FPM log tab](/assets/screenshots/site-detail-phpfpm.png)

- **App logs tab**: parses every `*.log` file the framework declares (Laravel: `storage/logs/*.log`) into level-coloured entries with click-to-expand stack traces and a live-search box. The dropdown switches between log files; the Latest / All toggle controls how many entries to fetch.

  ![App Logs tab on the site detail](/assets/screenshots/site-detail-applogs.png)
- **Env tab**: edit the project's `.env` (and any `.env.*` variant) right in the browser. Saving goes through a confirmation modal with an optional back-up-first checkbox and an atomic write that preserves the file mode; when a backup exists a **Restore** button opens a diff before rolling back. See [Environment Setup](./env-setup.md).
- **Edit nginx**: the sliders button at the end of the address bar opens the site's nginx override in a modal code editor with Save / Reset / Restore and timestamped backups, and every save runs `nginx -t` before committing. With a worktree tab selected it edits that worktree's override instead of the main branch's. See [Nginx Overrides](../usage/nginx-overrides.md).
- **Service badges**: beneath the path / git branch line, every service from the project's `.lerd.yaml` is shown as a small pill (green when running, grey when stopped). Click any badge to jump to that service's detail panel on the Services tab.

## Services

![Services tab](/assets/screenshots/services-list.png)

The middle panel lists core infrastructure services (MySQL, Redis, PostgreSQL, Meilisearch, RustFS, Mailpit), any installed preset alternates (e.g. MySQL 5.7, MariaDB 11, MongoDB) and admin UIs (phpMyAdmin, pgAdmin, Mongo Express), plus grouped per-site workers (Queues, Horizon, Schedules, Workers, Stripe, Reverb).

The header has a **+** button that opens the **preset picker modal**: a one-click installer for the bundled service presets. Multi-version presets like `mysql` and `mariadb` show a version dropdown next to the **Add** button. Already-installed entries are filtered out.

![Service preset picker modal](/assets/screenshots/preset-picker-modal.png)

Selecting a service opens the detail panel with Start, Stop, and Restart controls, status, and the correct `.env` connection values with a one-click copy button. Restart is available for every built-in and custom service and wraps `podman restart` (clears the paused flag on success); the grouped per-site workers (Queues, Horizon, Schedules, Workers, Stripe, Reverb) remain start/stop only. A **Check for updates** action sits in the service's action menu (non-worker services only); it bypasses the cached availability lookup, re-fetches the registry tag list, and shows either an "Already up to date" hint or a "Update available: {tag}" banner that becomes the live Update button moments later. Database service detail panels (mysql, postgres, mongo, and any installed alternate like `mysql-5-7`) get two extras:

- **Suggestion banner**: a sky-blue tip offering to install the paired admin UI (phpMyAdmin / pgAdmin / Mongo Express) when it isn't installed yet. Dismissable per-preset; dismissal persists in `localStorage`.
- **Open admin button**: when the paired admin UI is installed, a button on the header opens its dashboard inline as a full-width iframe overlay and auto-starts the admin service if needed. When no admin UI is installed and the service is active, a fallback **Open connection URL** anchor hands the `mysql://` / `postgresql://` / `mongodb://` URL to your registered DB client (DBeaver, TablePlus, Compass, etc.).
- **Dashboard button**: for any service that exposes a dashboard URL (Mailpit, RustFS, Meilisearch, phpMyAdmin, etc.), a Dashboard button in the header opens it as an inline full-width iframe. The iframe overlay has its own header with the service URL, an **Open in new tab** escape hatch, and a close button. Clicking one of the main nav icons (Sites / Services / System) also closes the overlay.

Services with a tuning mount (mysql, mariadb, redis, postgres, and any custom service that declares a `tuning:` block) also get a **Config** tab on their detail panel. It edits the runtime tuning override in a code editor with the same Save / Reset / Revert / Restore-from-backup flow as the nginx and `.env` editors; saving restarts the service so it re-reads the file. See [Tuning a service](../usage/custom-services.md#tuning-a-service).

## System

![System tab](/assets/screenshots/system.png)

The middle panel lists individual system components: DNS, Nginx, Watcher, each installed PHP-FPM version, each installed Node.js version, the Node install form, Autostart toggle, and the Lerd version entry.

Selecting an item opens its detail panel:

- **PHP-FPM cards**: show which sites use the version, Xdebug toggle with an inline mode selector (debug, coverage, debug-plus-coverage, develop, profile, trace, gcstats, visible when Xdebug is on), custom extension list, and a live FPM log stream. For versions with no active sites, a manual Start/Stop button is shown.
- **Edit php.ini**: each PHP version's detail has a **php.ini** tab (next to Logs and Sites) that edits that version's user `php.ini` override in a code editor, with Save / Reset / Revert and timestamped backups you can **Restore**. Changes apply to the shared FPM container for that version. The same file is reachable from the CLI via `lerd php:ini <version>`.
- **Install a PHP version**: a **+** button sits after the last PHP version tab (browser-tab style). It opens a modal with a dropdown of the supported versions that are not already installed; picking one and clicking Install builds the FPM image and streams the build log live. The build runs server-side, so closing the modal does not cancel it, and an *operation finished/failed* notification fires when it is done (see [Notifications](./notifications.md)).
- **Site Xdebug button**: every PHP site detail shows an Xdebug (bug) button in the address-bar row, between the LAN-share and terminal buttons. It turns green when Xdebug is on for that site's PHP version, and clicking it toggles Xdebug on or off in place (restarting the shared FPM container for that version). The button is hidden for static sites, custom containers, and FrankenPHP sites.
- **Node.js cards**: show which sites use the version, with a remove button. The **Install Node.js version** entry has an inline form; enter a version number (e.g. `22`) and click **Install**, equivalent to `lerd node:install <version>`.
- **Nginx card**: a **Logs** tab streaming the `lerd-nginx` container log, and a **Config** tab that edits the global http-level nginx override (gzip, proxy buffers, a global `client_max_body_size`, custom `map` blocks) with the same Save / Reset / Restore / backup flow as the per-site editor; every save runs `nginx -t` first. See [Nginx Overrides](../usage/nginx-overrides.md#scope).
- **Watcher card**: shows whether `lerd-watcher` is running; a Start button appears when stopped. Streams live watcher logs (DNS repair events, fsnotify errors, worktree timeouts).
- **Notifications card**: per-category toggles (mail captured, worker failures, finished service operations, service updates, possible N+1 queries, dumps), a *Send a test notification* button, and the list of subscribed browsers with *Forget* actions. See [Notifications](./notifications.md).
- **Autostart card**: enable or disable automatic start of all services at login.
- **Lerd card**: shows the current version and a **Check for updates** button. The status dot next to the entry is green when DNS, nginx, and the watcher are all running, red when any of them is down, and yellow when an update is available. When an update is available, an **Open terminal & update** button spawns the user's preferred terminal emulator with `lerd update` pre-filled (loopback only, the host needs to prompt for sudo). A small yellow dot also appears on the lerd logo in the left rail; clicking the logo always returns to the Dashboard, where the same update banner is surfaced on the Lerd widget.

The **Start** / **Stop** buttons in the System panel header start or stop all core services (DNS, nginx, and all PHP-FPM containers for versions that have active sites).

## Updates

Shows the current version. When an update is available, the Lerd entry exposes an **Open terminal & update** button that launches your terminal emulator running `lerd update`. The update requires `sudo` for sysctl/sudoers steps and so needs an interactive terminal; the button is loopback-only and is hidden when the dashboard is reached over the LAN.
