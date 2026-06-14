# FrankenPHP Runtime

Lerd can serve a PHP site through a per-site [FrankenPHP](https://frankenphp.dev/) container instead of the shared PHP-FPM container. FrankenPHP keeps PHP resident in memory, serves HTTP directly, and supports a worker mode that reuses a single PHP process across requests.

The FrankenPHP runtime is opt-in, framework-agnostic, and coexists with the default FPM runtime on the same machine. Laravel sites (via Octane) and Symfony sites (via the native worker flag) are both supported out of the box; any other PHP framework with a `public/index.php` gets the generic `frankenphp php-server` entrypoint.

---

## Switching runtime

Two equivalent ways to turn FrankenPHP on for a site.

**From `.lerd.yaml`** (commits the choice to the repo, so everyone who links the project gets the same runtime):

```yaml
runtime: frankenphp
runtime_worker: true
```

**From the CLI**:

```bash
cd ~/Code/my-app
lerd runtime frankenphp --worker
```

Flip back to FPM with `lerd runtime fpm`. `lerd runtime` without an argument prints the current runtime. Both surfaces restart the container, regenerate the nginx vhost, and reload nginx automatically.

---

## What happens under the hood

For a FrankenPHP site, lerd:

1. Pulls `dunglas/frankenphp:php<version>-alpine` for the site's PHP version (defaults 8.2, 8.3, 8.4; unsupported versions fall back to 8.4).
2. Writes a per-site quadlet `lerd-fp-<site>.container` that mounts the project at its host path and runs the framework's entrypoint.
3. Generates an nginx vhost that reverse-proxies to `lerd-fp-<site>:8000`.
4. Starts the container, reloads nginx.

The container joins the shared `lerd` Podman network, so services like `lerd-mysql`, `lerd-redis`, and `lerd-meilisearch` are reachable by hostname.

Pause semantics mirror FPM: `lerd pause <site>` (or the dashboard pause toggle, or `site_control action=pause`) stops `lerd-fp-<site>` alongside the paused-vhost swap, and `lerd unpause <site>` starts it again. The container is only running while the site is active, so a long-idle FrankenPHP site doesn't keep a process resident.

---

## Framework adapters

Each framework can declare how to launch FrankenPHP via a `frankenphp:` block in its definition. Both built-in adapters ship with one.

**Laravel** has two modes:

- **Non-worker (`runtime_worker: false`, default)**: lerd runs `frankenphp php-server -r public/`. Each request boots Laravel from scratch; code edits take effect on the next request, same as FPM. You still get FrankenPHP's HTTP/2, HTTP/3, and TLS, but not Octane's per-request speedup.
- **Worker (`runtime_worker: true`)**: lerd runs `php artisan octane:start --server=frankenphp --host=0.0.0.0 --port=8000 --workers=auto`. Octane keeps Laravel resident; requests skip the full bootstrap. Octane registers Symfony Console signal handlers which need the `pcntl` PHP extension; it is baked into lerd's derived FrankenPHP image (see the Extensions section) so the container boots straight into Octane.

**Symfony** uses FrankenPHP's native worker flag:

```
frankenphp php-server -l :8000 -r public/ [--worker=public/index.php --watch]
```

In worker mode lerd also passes `--watch`, which reloads the resident worker on any `.php`, `.env`, `.yaml`, or `.twig` change, so file edits take effect without a manual restart. `runtime/frankenphp-symfony` (optional) plugs Symfony's Runtime into the same worker loop for slightly lower per-request overhead.

**Any other framework** with a `public/index.php` falls back to:

```
frankenphp php-server -l :8000 -r <public_dir>
```

To override the defaults for a specific project, add a user framework overlay at `~/.config/lerd/frameworks/<name>.yaml` with a `frankenphp:` block, or commit a full framework definition alongside the project.

---

## Workers

Queue workers, schedulers, Reverb, Horizon, and any framework-defined worker continue to work unchanged: lerd spawns each as its own systemd service and `podman exec`s into the FrankenPHP container for the site. Laravel `queue:work` and Symfony `messenger:consume` both run alongside the web worker without conflict.

Start a worker the same way you would on an FPM site:

```bash
cd ~/Code/my-app
lerd worker start queue       # Laravel
lerd worker start messenger   # Symfony
```

---

## Worker mode on vs off

Both modes use the same FrankenPHP binary, so you always get HTTP/2, HTTP/3, and TLS for free. The difference is what happens *inside* the PHP process for each request.

**Worker off** (default): each incoming request runs `public/index.php` from scratch. The framework boots (container, DI, config cache, routes, middleware stack, etc.) on every hit, same as classic PHP-FPM. Memory resets between requests; file edits take effect on the next request.

**Worker on**: FrankenPHP keeps one resident PHP process alive and calls `frankenphp_handle_request()` in a loop. The framework boots **once**, then the warm worker handles every subsequent request by reusing the already-constructed DI container, cached routes, resolved config, etc. Requests are typically 10x to 50x faster because you skip the bootstrap each time.

Tradeoffs of worker mode:

- **State leaks across requests.** Anything you stored in a static property, a singleton service, or the global `$_SERVER` / `$_SESSION` arrays from request A is still there for request B. This is usually fine for well-written frameworks (Octane's "state resetters" and Symfony's Runtime handle the common cases), but custom code that assumes a fresh process per request can misbehave.
- **File edits are not picked up automatically.** The worker holds PHP in memory, so editing a controller doesn't affect the next request until the worker reloads. Symfony worker mode passes `--watch` so edits reload the worker within a second or two; Laravel worker mode reloads when you opt in with `lerd octane:reload on` (see [Dev iteration and hot reload](#dev-iteration-and-hot-reload)), otherwise it needs `lerd restart <site>` or `lerd runtime fpm`.
- **Memory usage grows over time.** Leaks that would be invisible in FPM (where each request gets a fresh process) become visible over thousands of requests.

Typical usage:

- **Local dev, iterating on code**: worker off, or Symfony worker on (auto reload). Laravel dev is usually happier with worker off or the shared FPM runtime.
- **Benchmarking, perf testing, staging**: worker on — this is the realistic production picture.
- **CI / ephemeral environments**: worker off — simpler, no state-leak surprises.

---

## Dev iteration and hot reload

Non-worker mode (the default) serves each request with a fresh PHP request lifecycle for both Laravel and Symfony, so file edits take effect on the next request, just like FPM. That's the right default for local iteration.

Worker mode keeps PHP resident, so a source file change is **not** picked up on the next request unless the worker is told to reload:

- **Symfony** worker mode passes `--watch` to `frankenphp php-server`, so edits under the project tree reload the worker within a second or two.
- **Laravel** worker mode is opt-in for auto-reload:

  ```bash
  cd ~/Code/my-app
  lerd octane:reload on    # serve via octane:start --watch
  lerd octane:reload off   # back to standard worker mode
  lerd octane:reload       # print the current state
  ```

  When on, lerd serves the site with `octane:start --watch` so edits restart the resident workers within a second or two. The toggle is also a refresh button next to the **Octane** segment in the Web UI site controls. Two prerequisites are handled for you:

  - Octane's file watcher runs under `node` and resolves `chokidar` from the project. Reload-on stays off until `chokidar` is installed; the CLI and the Web UI both offer a one-click `npm install -D chokidar` (Vite 8 no longer ships it transitively). Node is baked into lerd's derived FrankenPHP image, so the watcher works without an install step at boot.
  - On macOS (and WSL2 `/mnt` projects) the container can't observe host filesystem events, so lerd appends `--poll` automatically.

  If you'd rather not enable reload, the older workarounds still apply: `lerd restart <site>` (~5s), `php artisan octane:reload` inside the project (drops warm workers without restarting the container), or `lerd runtime frankenphp --no-worker` to hot-reload every request like FPM.

---

## Extensions and debug tooling

lerd builds a derived image, `localhost/lerd-frankenphp<version>:local`, FROM the dunglas base with the same runtime extension set the FPM image ships (redis, gd, pdo_mysql/pgsql, intl, imagick, igbinary, mongodb, gmp, bcmath, soap, ldap, zip, ...), plus any extensions and packages you add globally. They are compiled for the ZTS runtime and baked once, so `pcntl` and `nodejs` are present from first boot rather than installed at container start. The image rebuilds automatically when lerd's definition changes or via `lerd php:rebuild`.

The per-request debug tooling works for requests Octane serves too: lerd bind-mounts the same dump bridge, `lerd_devtools` (the Debug window's query/job/view/mail/event/http capture), and Xdebug config into the FrankenPHP container. `lerd dump on`, the Debug window, and the Xdebug toggle all apply to a FrankenPHP site. `dump()`/`dd()` and captured queries from a live Octane request land in the dashboard exactly as they do under FPM.

CLI tooling (`lerd test`, `lerd pest`, `lerd php:bun`, `lerd pest:browser`, `php`, `composer`) execs into the shared FPM container for the site's PHP version, so bun and Pest browser testing work for FrankenPHP sites with no extra setup.

**php.ini** is edited per site on FrankenPHP, not per PHP version. A FrankenPHP site runs its own container, so it has its own `php.ini` file edited from a **php.ini tab in the site's config modal** (the gear/Nginx button); the change applies to that site only and restarts just its container. This is different from FPM sites, which share one per-version `php.ini` edited under System → PHP. The System → PHP per-version editor does not affect FrankenPHP sites.

## What is not supported on FrankenPHP

Everything the FPM runtime offers works on FrankenPHP **except one thing**:

- **SPX profiler** (`lerd profile`). SPX profiles per request and does not hook Octane's resident-worker loop (its `/_spx` UI 404s under Octane, and lerd's profiler injection is fastcgi-only), so it stays FPM-only. The global profiler toggle still profiles your FPM sites; to profile a FrankenPHP site with SPX, switch it back to FPM (`lerd runtime fpm`, run from the project).

Everything else — the full runtime extension set, Xdebug, the dump()/dd() bridge, the Debug window (`lerd_devtools`), per-site php.ini, bun, and Pest browser testing — is supported.

## Other notes

- **PHP version picker** (in the Web UI and `lerd isolate`) rebuilds the derived image for the matching `dunglas/frankenphp:php<version>-alpine` base and restarts the site. Versions without published dunglas images fall back to the latest.
- **macOS** works the same way as Linux because FrankenPHP runs inside the Podman Machine VM; no extra wiring required.

---

## Runtime badge

The Web UI site detail panel shows an orange **FrankenPHP** badge next to the framework and services, with a `worker` suffix when worker mode is on. The same badge appears in `lerd tui` beside the PHP version line.

---

## Related pages

- [Per-project custom container](project-setup.md#custom-container) — for non-PHP apps or a fully custom image.
- [Web UI](web-ui.md) and [TUI](tui.md) — where the runtime badge appears.
- [Environment setup](env-setup.md) — `.env` wiring is identical under both runtimes.
