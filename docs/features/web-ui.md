# Web UI

Lerd includes a browser dashboard available at **`http://lerd.localhost`**, served by the `lerd-ui` systemd service (started automatically with `lerd install`).

```bash
lerd dashboard   # open in your default browser
```

The `.localhost` TLD resolves to `127.0.0.1` natively on all modern systems — no DNS configuration needed. The dashboard is also reachable directly at `http://127.0.0.1:7073` if nginx is not running.

## Install as an app

The dashboard is a Progressive Web App (PWA). You can install it as a standalone desktop app from any Chromium-based browser (Chrome, Brave, Edge):

1. Open `http://lerd.localhost`
2. Click the **install** icon (⊕) in the address bar
3. Click **Install**

Once installed, Lerd opens in its own window without browser chrome, just like a native app.

---

## Layout

The dashboard uses a three-pane layout:

- **Left icon rail** — switch between Sites, Services, and System with icon buttons; theme toggle and docs link at the bottom
- **Middle list panel** — scrollable list of all items in the active section; status dots, compact rows, collapsible groups
- **Detail panel** — full controls and live logs for the selected item

On mobile the list and detail panels are full-screen with a bottom tab bar for navigation.

---

## Sites

![Sites tab](/assets/screenshots/app-1.png)

The middle panel lists all registered projects. Active sites show a status dot (green when FPM is running), domain name, and small indicator dots for running workers (amber for queue/horizon, sky for reverb, emerald for schedule, violet for custom workers). Paused sites appear in a separate collapsible section.

Selecting a site opens the detail panel with:

- **HTTPS toggle** — enable or disable TLS with one click; updates `APP_URL` in `.env` automatically
- **PHP / Node dropdowns** — change the version per site; writes `.php-version` / `.node-version` into the project and regenerates the nginx vhost on the fly
- **Queue toggle** — start or stop the queue worker; amber when running; live log stream below
- **Schedule toggle** — start or stop the task scheduler; live log stream below
- **Reverb toggle** — start or stop the Reverb WebSocket server; only shown when the project uses Reverb (detected via composer or `.env`)
- **Framework worker toggles** — additional workers defined by the site's framework (e.g. Symfony `messenger`, Laravel `horizon`) appear as indigo toggles
- **Stripe toggle** — start or stop the Stripe webhook listener
- **Pause / Resume** — suspend a site's nginx vhost without unlinking it; the site stays registered and FPM keeps running
- **Unlink button** — remove a site from nginx without touching the terminal
- **Git Worktrees** — when the project uses git worktrees, each branch and its domain are listed with a direct open link
- **Live PHP-FPM log** — streams FPM output for the selected site; tab switches to queue/horizon/schedule/reverb logs when those workers are running
- **Service badges** — beneath the path / git branch line, every service from the project's `.lerd.yaml` is shown as a small pill (green when running, grey when stopped). Click any badge to jump to that service's detail panel on the Services tab.

## Services

![Services tab](/assets/screenshots/app-2.png)

The middle panel lists core infrastructure services (MySQL, Redis, PostgreSQL, Meilisearch, RustFS, Mailpit), any installed preset alternates (e.g. MySQL 5.7, MariaDB 11, MongoDB) and admin UIs (phpMyAdmin, pgAdmin, Mongo Express), plus grouped per-site workers (Queues, Horizon, Schedules, Workers, Stripe, Reverb).

The header has a **+** button that opens the **preset picker modal**: a one-click installer for the bundled service presets. Multi-version presets like `mysql` and `mariadb` show a version dropdown next to the **Add** button. Already-installed entries are filtered out.

Selecting a service opens the detail panel with start/stop controls, status, and the correct `.env` connection values with a one-click copy button. Database service detail panels (mysql, postgres, mongo, and any installed alternate like `mysql-5-7`) get two extras:

- **Suggestion banner** — a sky-blue tip offering to install the paired admin UI (phpMyAdmin / pgAdmin / Mongo Express) when it isn't installed yet. Dismissable per-preset; dismissal persists in `localStorage`.
- **Open admin button** — when the paired admin UI is installed, a button on the header opens its dashboard in a new tab and auto-starts the admin service if needed. When no admin UI is installed and the service is active, a fallback **Open connection URL** anchor hands the `mysql://` / `postgresql://` / `mongodb://` URL to your registered DB client (DBeaver, TablePlus, Compass…).

## System

![System tab](/assets/screenshots/app-3.png)

The middle panel lists individual system components: DNS, Nginx, Watcher, each installed PHP-FPM version, each installed Node.js version, the Node install form, Autostart toggle, and the Lerd version entry.

Selecting an item opens its detail panel:

- **PHP-FPM cards** — show which sites use the version, Xdebug toggle, custom extension list, and a live FPM log stream. For versions with no active sites, a manual Start/Stop button is shown.
- **Node.js cards** — show which sites use the version, with a remove button. The **Install Node.js version** entry has an inline form — enter a version number (e.g. `22`) and click **Install**, equivalent to `lerd node:install <version>`.
- **Watcher card** — shows whether `lerd-watcher` is running; a Start button appears when stopped. Streams live watcher logs (DNS repair events, fsnotify errors, worktree timeouts).
- **Autostart card** — enable or disable automatic start of all services at login.
- **Lerd card** — shows the current version and a **Check for updates** button. When an update is available, the version number and a `lerd update` instruction are shown.

The **Start** / **Stop** buttons in the System panel header start or stop all core services (DNS, nginx, and all PHP-FPM containers for versions that have active sites).

## Updates

Shows the current version. When an update is available, a notice with the version number is shown alongside an instruction to run `lerd update` in a terminal (the update requires `sudo` for sysctl/sudoers steps and cannot run in the background).
