# PHP

## Commands

| Command | Description |
|---|---|
| `lerd use <version>` | Set the global PHP version and build the FPM image if needed |
| `lerd isolate <version>` | Pin PHP version for cwd: writes `.php-version` and updates `.lerd.yaml` if it exists, then re-links |
| `lerd php:list` | List all installed PHP-FPM versions |
| `lerd php:rebuild [--local]` | Force-rebuild all installed PHP-FPM images; `--local` builds from source instead of pulling a base |
| `lerd fetch [version...] [--local]` | Pull pre-built PHP FPM base images from ghcr.io; `--local` builds from source instead |
| `lerd xdebug on [version] [--mode MODE] [--on-demand]` | Enable Xdebug for a PHP version with the given mode (default `debug`) and restart the FPM container. `--on-demand` sets `start_with_request=trigger` so nothing auto-connects |
| `lerd xdebug off [version]` | Disable Xdebug and restart the FPM container |
| `lerd xdebug status` | Show Xdebug enabled/disabled state and active mode for all installed PHP versions |
| `lerd xdebug pause [site] [--list] [--pid PID]` | Break the IDE debugger into a running worker/CLI process via Xdebug's control socket. `--list` shows candidate processes |
| `lerd php:ext add <ext> [version] [--apk-deps "pkg ..."]` | Add a custom PHP extension to the FPM image and rebuild; `--apk-deps` lists extra Alpine packages the extension needs to build |
| `lerd php:ext remove <ext> [version]` | Remove a custom PHP extension and rebuild |
| `lerd php:ext list [version]` | List custom extensions configured for a PHP version |
| `lerd php:bun install [version]` | Install a musl bun inside the PHP-FPM container, into a persistent volume |
| `lerd php:bun remove` | Remove the in-container bun and clear its shared persistent volume |
| `lerd php:bun update [version]` | Update the container's bun in place (`bun upgrade`) |
| `lerd php:bun version [version]` | Show the bun version installed in the container |
| `lerd php:pkg add <package...> [--php version]` | Install extra Alpine packages into the FPM image and rebuild |
| `lerd php:pkg remove <package...> [--php version]` | Remove extra Alpine packages and rebuild |
| `lerd php:pkg list [--php version]` | List the extra packages configured for a PHP version |
| `lerd pest:browser install [version]` | Set up in-container Pest browser testing (musl chromium + Playwright shim); see [browser testing](browser-testing#pest-browser-testing-playwright) |
| `lerd pest:browser remove [version]` | Remove chromium from the FPM image and disable Pest browser testing |
| `lerd pest:browser doctor [version]` | Diagnose the Pest browser testing setup for a PHP version |
| `lerd php:ini [version]` | Open the user php.ini for a PHP version in `$EDITOR` |

If no version is given, the version is resolved from the current directory (`.php-version` or `composer.json`, falling back to the global default).

---

## Usage

`lerd install` places shims for `php` and `composer` in `~/.local/share/lerd/bin/`, which is added to your `PATH`. You use them exactly as you normally would, lerd routes them through the correct PHP-FPM container version automatically:

```bash
php artisan migrate
composer install
```

Because the `php` shim runs inside the PHP-FPM container, `php artisan`, `lerd artisan`, and the MCP `exec` tool's `artisan` action are all equivalent; they all execute inside the same container with the same PHP version and extensions. Use whichever form you prefer.

### Shortcuts and `vendor/bin` fallback

For common workflows there are a few built-in shortcuts:

- `lerd a [args...]`: short alias for `lerd artisan` (also `lerd console`)
- `lerd test [args...]`: runs `lerd artisan test`

In addition, any composer-installed binary in the project's `vendor/bin` directory is callable directly as `lerd <name>`. For example, with the usual Laravel dev tooling installed:

```bash
lerd pest
lerd pint
lerd phpstan analyse
lerd rector process
```

These run inside the project's PHP-FPM container with the project's working directory mounted, so configuration files (`pest.xml`, `pint.json`, `phpstan.neon`, etc.) are picked up automatically. Real lerd commands always take precedence; if you have a `vendor/bin/composer`, `lerd composer` still resolves to the built-in command.

The MCP integration exposes the same surface through two tools, `vendor_bins` (list available binaries) and `vendor_run` (execute one), so AI assistants can discover and run project tooling without per-project configuration.

---

## Version resolution

When serving a request, Lerd picks the PHP version for a project in this order:

1. `.lerd.yaml` in the project root: `php_version` field (explicit lerd override)
2. `.php-version` file in the project root (plain text, e.g. `8.2`)
3. `composer.json`: `require.php` constraint, resolved to the best installed version (e.g. `^8.4` with PHP 8.4 and 8.5 installed resolves to `8.5`)
4. Global default in `~/.config/lerd/config.yaml`

When `.php-version` changes on disk, the lerd watcher automatically updates the site registry and regenerates the nginx vhost, no manual reload needed.

To pin a project permanently:

```bash
cd ~/Lerd/my-app
lerd isolate 8.5
```

This writes `.php-version: 8.5` (so CLI `php`, asdf, and other tools see the right version) and, when `.lerd.yaml` already exists in the project, also updates its `php_version` field to keep lerd's priority-1 override in sync. The site is re-linked automatically so nginx picks up the new version immediately.

The UI PHP version selector and the MCP `site` tool's `php` action follow the same rules; they always write both files when applicable.

The composer constraint is matched against all installed PHP versions using full semver rules (`^`, `~`, `>=`, `<`, `||`, `*`). The highest installed version that satisfies the constraint wins. If no installed version matches, the literal minimum from the constraint is used (and the FPM will be built on first use).

::: tip Overriding a `composer.json` constraint
If `composer.json` requires `^8.3` but you need to run the project on a specific version, `lerd isolate 8.5` is the right tool. It writes `.php-version` which takes priority over the composer constraint. Running `lerd use 8.5` alone won't help; that only sets the global fallback, which loses to the composer constraint.
:::

To change the global default (applies to all projects that don't have a per-project pin):

```bash
lerd use 8.5
```

---

## FPM lifecycle

Lerd automatically manages which PHP-FPM containers are running based on which versions are actually needed by your sites.

**`lerd start`**: only starts FPM containers for versions referenced by at least one site (active or paused). Unused versions are left stopped.

**Auto-stop**: when you unlink a site, lerd checks every installed PHP version. If no remaining active (non-ignored, non-paused) site uses a version, its FPM container is stopped. The version itself stays installed; the container is just not running.

**Paused sites count**: a site that is paused still counts as using its PHP version, so that version's FPM container is not stopped. When the site is resumed, FPM is guaranteed to be running.

**Auto-start**: FPM is started automatically when you link a site (`lerd link`, `lerd park`, `lerd isolate`) or change the global default (`lerd use`). When unpausing a site, lerd also ensures the required FPM container is running before restoring the nginx vhost.

**Manual control**: unused PHP versions (no active sites) can be started and stopped manually from the dashboard (System > PHP > Start / Stop). From the CLI:

```bash
systemctl --user start  lerd-php84-fpm
systemctl --user stop   lerd-php84-fpm
```

**`lerd status`**: stopped FPM containers for unused versions are reported as a warning, not an error.

---

## Xdebug

::: details Xdebug configuration values
Xdebug is configured with:

- `xdebug.mode=<mode>` (defaults to `debug`, configurable per PHP version)
- `xdebug.start_with_request=yes` (or `trigger` with `--on-demand`)
- `xdebug.client_host=host.containers.internal` (reaches your host IDE from the container)
- `xdebug.client_port=9003`

Set your IDE to listen on port `9003`. In VS Code, the default PHP Debug configuration works without changes. In PhpStorm, set **Settings > PHP > Debug > Debug port** to `9003`.

`host.containers.internal` is resolved via a real reachability probe: when lerd writes the shared hosts file it tries each candidate IP (netavark's `host.containers.internal` entry, the host's primary LAN IP, slirp4netns's `10.0.2.2`) by opening a TCP connection to lerd-ui on port 7073 from inside lerd-nginx, and writes the first one that succeeds. If none succeed, `lerd doctor` reports the failure so you get a real diagnosis instead of Xdebug silently timing out with `Time-out connecting to debugging client`.
:::

### Picking a mode

Xdebug supports several modes: `debug` (step debugging, the default), `coverage` (code coverage collection), `develop`, `profile`, `trace`, `gcstats`, and `off`. Pick one with `--mode`:

```bash
lerd xdebug on --mode coverage        # code coverage for phpunit / pest
lerd xdebug on --mode debug,coverage  # both at once
lerd xdebug on 8.4 --mode trace       # explicit version
```

When combined with PCOV this matters in one direction: if your test runner's `phpunit.xml` prefers PCOV it still wins for coverage, but once you enable Xdebug in `coverage` mode your runner can fall back to Xdebug when PCOV isn't available or is disabled (`pcov.enabled = 0` in `lerd php:ini`). Running Xdebug in `coverage` mode carries the usual runtime cost, so only switch while you actually need coverage.

Re-run `lerd xdebug on --mode <new>` at any time to swap modes without going through `off` first.

### On-demand debugging (workers and CLI)

By default `start_with_request=yes`, so with the debugger listening every request and every running worker tries to connect at once. To debug a single process on demand instead, enable on-demand mode and attach with `pause`:

```bash
lerd xdebug on --on-demand        # start_with_request=trigger — nothing auto-connects
lerd xdebug pause --list          # list running PHP processes that expose a control socket
lerd xdebug pause --pid 1234      # break the IDE into that process
```

`pause` uses Xdebug's [control socket](https://xdebug.org/docs/xdebugctl) (Xdebug >= 3.3, baked into lerd's FPM images) via the `xdebugctl` tool. It is the practical way to debug a **queue/Horizon worker, a scheduled task, or a CLI script** — processes where you can't set a trigger cookie. Run it from a project directory (or pass a site name); lerd resolves the site's container, scopes the candidate list to that site's own processes, and tells the running process to connect to your IDE on port `9003`. The worker must have been started *after* Xdebug was enabled, and your IDE must be listening.

For ordinary web requests under `--on-demand`, use the [Xdebug Helper](https://xdebug.org/docs/step_debug#browser-extensions) browser extension (or append `?XDEBUG_TRIGGER=1`) to trigger a session per page.

---

## Debug bridge

Calls to `dump()` and `dd()` can be captured into the lerd dashboard, TUI, and MCP tools instead of (or alongside) the response. Enable with:

```bash
lerd dump on        # touch the sentinel; next request captures
lerd dump tail      # follow the live feed
lerd dump off       # remove the sentinel; subsequent requests are no-ops
```

Toggling never restarts FPM or its workers. The bridge auto-prepend file and its conf.d ini are always mounted into every FPM container; the on/off state lives in a runtime sentinel the bridge stats on each request. By default the bridge captures only and the HTTP response stays clean. Set `dumps.passthrough: true` in `config.yaml` to also keep the original `sf-dump` output in the response. See the [Dump viewer feature page](../features/dumps.md) for the wire format, the surfaces (per-site tab, System sidebar, antenna toggle), and tuning knobs.

---

## Pre-built images

lerd ships pre-built PHP-FPM base images on ghcr.io for all supported versions (7.4 and 8.0–8.5), covering both `amd64` and `arm64`. When you run `lerd fetch` or `lerd php:rebuild`, lerd pulls the matching base image and layers just your mkcert CA certificate on top, bringing first-time build time from ~5 minutes down to ~30 seconds.

The base image tag is derived from the embedded Containerfile, so lerd always pulls the exact image that matches the version of lerd you have installed. If the pull fails (no internet, image not yet published) lerd falls back to a full local build transparently.

The images are public, so no ghcr.io login is required. lerd pulls them anonymously even if you are already logged into ghcr.io, to avoid authentication errors from expired or unrelated credentials.

`lerd start` checks all required images before starting containers. If any are missing (e.g. after `podman image rm`), it rebuilds or pulls them automatically using the same parallel spinner UI, so containers always start against a valid image.

To build entirely from source instead:

```bash
lerd fetch --local
lerd fetch --local 8.5
lerd php:rebuild --local
```

---

## Legacy PHP versions

PHP 7.4 and 8.0 are available as a frozen legacy tier for old projects (Laravel 6–8 on 7.4, Laravel 8–9 on 8.0). They build from the same Alpine-based recipe as the current versions, including ICU full locale data, but with a few caveats:

- They are end-of-life upstream and get no security patches. Use them only for local work on legacy apps.
- Xdebug is pinned to the last release supporting that PHP line (3.1.6 for 7.4, 3.3.2 for 8.0).
- The `mongodb` extension is unavailable (it requires PHP 8.1+); everything else in the standard bundle is present.
- The base image is Alpine 3.16, so the bundled Node.js is 16.x.

Use them like any other version:

```bash
lerd use 7.4
lerd isolate 8.0
lerd fetch 7.4 8.0
```

---

## Custom extensions

The default lerd FPM image ships ~30 extensions covering the vast majority of Laravel projects (`bcmath`, `bz2`, `calendar`, `curl`, `dba`, `exif`, `gd`, `gmp`, `igbinary`, `imagick`, `intl`, `ldap`, `mbstring`, `mongodb`, `mysqli`, `opcache`, `pcntl`, `pdo_mysql`, `pdo_pgsql`, `pdo_sqlite`, `redis`, `soap`, `shmop`, `sockets`, `sqlite3`, `sysvmsg`, `sysvsem`, `sysvshm`, `xdebug`, `xsl`, `zip`, and more).

To add an extension that isn't in the bundle:

```bash
lerd php:ext add swoole          # uses detected/default PHP version
lerd php:ext add swoole 8.3      # explicit version
```

This rebuilds the FPM image with the extension installed and restarts the container. Extensions are persisted in `~/.config/lerd/config.yaml` so they survive `lerd php:rebuild`.

After the rebuild, lerd checks that the extension actually loaded (`php -m`); if the PECL build failed, `lerd php:ext add` exits with an error and removes the extension from the config again, rather than reporting success for an extension that isn't there.

Some extensions need extra Alpine packages to compile. lerd already knows the ones for `imap` (`imap-dev krb5-dev openssl-dev c-client`); for anything else, pass them with `--apk-deps`:

```bash
lerd php:ext add ssh2 --apk-deps "libssh2-dev"
lerd php:ext add imap                                  # deps known to lerd, no flag needed
```

The packages are saved alongside the extension in `~/.config/lerd/config.yaml` (under `php.ext_apk_deps`), so they reapply on every `lerd php:rebuild`.

```bash
lerd php:ext list                # show custom extensions (and their apk deps) for current version
lerd php:ext remove swoole       # remove and rebuild
```

### php.ini settings

Each PHP version has a user-editable ini file at `~/.local/share/lerd/php/<version>/98-lerd-user.ini`, mounted read-only into the FPM container. Edit it with:

```bash
lerd php:ini          # detected/default version
lerd php:ini 8.3      # explicit version
```

This opens the file in `$EDITOR` (falls back to `nano`/`vim`). After saving, restart FPM to apply:

```bash
systemctl --user restart lerd-php84-fpm
```

The file is created automatically with commented-out examples when lerd first sets up the PHP version.

### Locales and internationalisation

The FPM image is Alpine-based, so it uses musl libc rather than glibc. Two consequences worth knowing:

- **`ext-intl` (`NumberFormatter`, `IntlDateFormatter`, Laravel's `Number::currency()`, `money` formatting) works for every locale.** The image bundles ICU's full CLDR locale database (`icu-data-full`), so `new NumberFormatter('nl_NL', NumberFormatter::CURRENCY)` correctly produces `€ 13.943,20`. This is the recommended way to do locale-aware formatting and it does not depend on the system locale at all.
- **The C-library `setlocale()` / `localeconv()` path stays in the C locale.** musl does not implement locale-specific `LC_NUMERIC` / `LC_MONETARY` rules, so `setlocale(LC_ALL, 'nl_NL')` will return a value but `localeconv()` keeps returning `.` / empty separators, and `number_format()` without explicit separators won't switch. Pass separators explicitly (`number_format($n, 2, ',', '.')`) or use `ext-intl`.

If a library you depend on calls `setlocale()` and branches on whether it succeeded, adding the `musl-locales` / `musl-locales-lang` apk packages makes the call return a value, but it still will not change number or currency formatting.

---

## Custom image (Containerfile)

When `php:ext` and per-version ini tweaks are not enough and a single site needs its own bespoke image (an extra system toolchain, a patched binary, arbitrary build steps), you can give that PHP site its own `Containerfile.lerd`. lerd builds a per-site image and serves the site by fastcgi from a dedicated FPM container, instead of the shared `lerd-php<ver>-fpm` one. It is the same `container:` key used for [custom containers](../getting-started/containers.md), with one difference: **no port**. A `container:` block with a port is a reverse-proxied app; a `container:` block with no port on a PHP project is served by fastcgi from your image.

Your `Containerfile.lerd` must build `FROM` the lerd base image for the site's PHP version, so it keeps php-fpm, the bundled extensions, and the pool config. That `:local` tag is lerd-managed and rebuilt on updates, so the `FROM` stays valid:

```dockerfile
FROM lerd-php84-fpm:local
RUN apk add --no-cache htop vim
```

```yaml
# .lerd.yaml
domains:
  - myapp
container:
  containerfile: Containerfile.lerd
```

Then `lerd link`. lerd builds `lerd-custom-myapp:local`, runs a dedicated FPM container `lerd-cfpm-myapp`, and points nginx fastcgi at it. The per-site container reuses every lerd mount, so xdebug, dumps, the debug bridge, the profiler, and `lerd shell` all work exactly as on a normal PHP site, and `lerd php`, `artisan`, `composer`, `tinker`, and queue/horizon workers all run inside it. Toggling xdebug for that PHP version restarts the per-site container too.

The PHP version is fixed by the `FROM` line, not by `.php-version` or the dashboard, so the version selector is shown read-only for these sites. To change the version, edit the `FROM` and relink.

```bash
lerd rebuild        # rebuild the per-site image after editing Containerfile.lerd
lerd restart        # restart the container without rebuilding
```

::: info PHP projects only
A no-port `container:` is for PHP projects served by fastcgi. For a non-PHP app (Node, Python, Go) give the `container:` block a `port` so nginx reverse-proxies to it; see the [containers walkthrough](../getting-started/containers.md).
:::

::: warning One container per site
Each custom-image PHP site runs its own FPM container rather than sharing the per-version one, so it uses more memory in the Podman VM. Reach for it only when a site genuinely needs its own image; for adding an extension or a package to every site on a version, `php:ext` stays lighter.
:::

---

## PHP shell

`lerd shell` opens an interactive shell inside the PHP-FPM container for the current project:

```bash
lerd shell
```

The PHP version is resolved the same way as every other lerd command (`.php-version`, `composer.json`, global default). The shell's working directory is set to the project root.

If the container is not running, lerd prints the platform-appropriate command (`launchctl kickstart` on macOS, `systemctl --user start` on Linux) to bring it back up rather than silently failing.

If the site is paused, any services referenced in `.env` (MySQL, Redis, etc.) are started automatically before the shell opens; the site itself stays paused.

### Shell environment

The lerd PHP-FPM image ships zsh with a self-contained config (starship prompt, persistent history, sensible defaults). When you run `lerd shell` or open a shell from the TUI, lerd execs zsh inside the container; for non-PHP service containers (Redis, MySQL, etc.) the fallback chain is `zsh > bash > sh` depending on what the upstream image provides.

The in-container shell is deliberately isolated from your host shell config. Every developer's `~/.zshrc` or `~/.config/fish` is different, and sourcing distro-specific paths or host-only binaries inside the alpine container cascades into noisy errors (missing oh-my-zsh, missing pacman, missing fastfetch, etc.). Rather than play whack-a-mole, lerd ships a clean, predictable shell environment that's identical across every machine and contributor.

What you get inside the container:

- **starship** as the default prompt — branch, dir, git status, all the usual.
- **eza**, **bat**, **fzf**, **zoxide** on `$PATH` for nicer file listing, paging, fuzzy-find, and `cd` history.
- Shell history persisted under `~/.local/share/lerd/shell-state/php-<version>/zsh/history`, so commands survive container rebuilds.
- `HostName=` set to your host's hostname so the prompt reads `root@your-machine` instead of the auto-generated container id.

If you want extra packages in the image (additional CLI tools, language toolchains, etc.), use `lerd php:ext` for PHP extensions, or fork the Containerfile at `internal/podman/quadlets/lerd-php-fpm.Containerfile`.

For other tools and runtime libraries, `lerd php:pkg add <packages>` installs Alpine packages into the FPM image's runtime stage and rebuilds, for example `lerd php:pkg add htop vim`. The packages are saved in `~/.config/lerd/config.yaml` (under `php.packages`, keyed by version) and re-applied on every rebuild, so they survive `php:rebuild` and base image updates, exactly like custom extensions. They are layered onto the shared image rather than baked into the published base, so they only affect your local build. A non-existent package name fails the rebuild and the change is reverted.

For [bun](https://bun.sh) specifically, run `lerd php:bun install` to drop a musl bun into the container's persistent `/root/.bun` volume (so `lerd shell` has it without rebuilding the image). See [bun](node#bun) for the full host and container story.

---

### Composer.json detection

When you run `lerd park` or `lerd link`, Lerd reads `composer.json` and warns if any `ext-*` requirements are not covered by the bundled or installed extension set:

```
[!] my-app requires PHP extensions not in the image: swoole
    Run: lerd php:ext add swoole
```
