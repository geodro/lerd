# PHP

## Commands

| Command | Description |
|---|---|
| `lerd use <version>` | Set the global PHP version and build the FPM image if needed |
| `lerd isolate <version>` | Pin PHP version for cwd — writes `.php-version` and updates `.lerd.yaml` if it exists, then re-links |
| `lerd php:list` | List all installed PHP-FPM versions |
| `lerd php:rebuild [--local]` | Force-rebuild all installed PHP-FPM images; `--local` builds from source instead of pulling a base |
| `lerd fetch [version...] [--local]` | Pull pre-built PHP FPM base images from ghcr.io; `--local` builds from source instead |
| `lerd xdebug on [version]` | Enable Xdebug for a PHP version — rebuilds the FPM image and restarts the container |
| `lerd xdebug off [version]` | Disable Xdebug — rebuilds without Xdebug and restarts |
| `lerd xdebug status` | Show Xdebug enabled/disabled for all installed PHP versions |
| `lerd php:ext add <ext> [version]` | Add a custom PHP extension to the FPM image and rebuild |
| `lerd php:ext remove <ext> [version]` | Remove a custom PHP extension and rebuild |
| `lerd php:ext list [version]` | List custom extensions configured for a PHP version |
| `lerd php:ini [version]` | Open the user php.ini for a PHP version in `$EDITOR` |

If no version is given, the version is resolved from the current directory (`.php-version` or `composer.json`, falling back to the global default).

---

## Usage

`lerd install` places shims for `php` and `composer` in `~/.local/share/lerd/bin/`, which is added to your `PATH`. You use them exactly as you normally would — lerd routes them through the correct PHP-FPM container version automatically:

```bash
php artisan migrate
composer install
```

Because the `php` shim runs inside the PHP-FPM container, `php artisan`, `lerd artisan`, and the MCP `artisan` tool are all equivalent — they all execute inside the same container with the same PHP version and extensions. Use whichever form you prefer.

### Shortcuts and `vendor/bin` fallback

For common workflows there are a few built-in shortcuts:

- `lerd a [args...]` — short alias for `lerd artisan` (also `lerd console`)
- `lerd test [args...]` — runs `lerd artisan test`

In addition, any composer-installed binary in the project's `vendor/bin` directory is callable directly as `lerd <name>`. For example, with the usual Laravel dev tooling installed:

```bash
lerd pest
lerd pint
lerd phpstan analyse
lerd rector process
```

These run inside the project's PHP-FPM container with the project's working directory mounted, so configuration files (`pest.xml`, `pint.json`, `phpstan.neon`, etc.) are picked up automatically. Real lerd commands always take precedence — if you have a `vendor/bin/composer`, `lerd composer` still resolves to the built-in command.

The MCP integration exposes the same surface through two tools, `vendor_bins` (list available binaries) and `vendor_run` (execute one), so AI assistants can discover and run project tooling without per-project configuration.

---

## Version resolution

When serving a request, Lerd picks the PHP version for a project in this order:

1. `.lerd.yaml` in the project root — `php_version` field (explicit lerd override)
2. `.php-version` file in the project root (plain text, e.g. `8.2`)
3. `composer.json` — `require.php` constraint, resolved to the best installed version (e.g. `^8.3` with PHP 8.3 and 8.4 installed → `8.4`)
4. Global default in `~/.config/lerd/config.yaml`

When `.php-version` changes on disk, the lerd watcher automatically updates the site registry and regenerates the nginx vhost — no manual reload needed.

To pin a project permanently:

```bash
cd ~/Lerd/my-app
lerd isolate 8.4
```

This writes `.php-version: 8.4` (so CLI `php`, asdf, and other tools see the right version) and, when `.lerd.yaml` already exists in the project, also updates its `php_version` field to keep lerd's priority-1 override in sync. The site is re-linked automatically so nginx picks up the new version immediately.

The UI PHP version selector and the MCP `site_php` tool follow the same rules — they always write both files when applicable.

The composer constraint is matched against all installed PHP versions using full semver rules (`^`, `~`, `>=`, `<`, `||`, `*`). The highest installed version that satisfies the constraint wins. If no installed version matches, the literal minimum from the constraint is used (and the FPM will be built on first use).

::: tip Overriding a `composer.json` constraint
If `composer.json` requires `^8.3` but you need to run the project on a specific version, `lerd isolate 8.4` is the right tool. It writes `.php-version` which takes priority over the composer constraint. Running `lerd use 8.4` alone won't help — that only sets the global fallback, which loses to the composer constraint.
:::

To change the global default (applies to all projects that don't have a per-project pin):

```bash
lerd use 8.4
```

---

## FPM lifecycle

Lerd automatically manages which PHP-FPM containers are running based on which versions are actually needed by your sites.

**`lerd start`** — only starts FPM containers for versions referenced by at least one site (active or paused). Unused versions are left stopped.

**Auto-stop** — when you unlink a site, lerd checks every installed PHP version. If no remaining active (non-ignored, non-paused) site uses a version, its FPM container is stopped. The version itself stays installed — the container is just not running.

**Paused sites count** — a site that is paused still counts as using its PHP version, so that version's FPM container is not stopped. When the site is resumed, FPM is guaranteed to be running.

**Auto-start** — FPM is started automatically when you link a site (`lerd link`, `lerd park`, `lerd isolate`) or change the global default (`lerd use`). When unpausing a site, lerd also ensures the required FPM container is running before restoring the nginx vhost.

**Manual control** — unused PHP versions (no active sites) can be started and stopped manually from the dashboard (System → PHP → Start / Stop). From the CLI:

```bash
systemctl --user start  lerd-php84-fpm
systemctl --user stop   lerd-php84-fpm
```

**`lerd status`** — stopped FPM containers for unused versions are reported as a warning, not an error.

---

## Xdebug

::: details Xdebug configuration values
Xdebug is configured with:

- `xdebug.mode=debug`
- `xdebug.start_with_request=yes`
- `xdebug.client_host=host.containers.internal` (reaches your host IDE from the container)
- `xdebug.client_port=9003`

Set your IDE to listen on port `9003`. In VS Code, the default PHP Debug configuration works without changes. In PhpStorm, set **Settings → PHP → Debug → Debug port** to `9003`.

`host.containers.internal` is resolved dynamically: when lerd writes the shared hosts file it probes the running `lerd-nginx` container for the IP that podman uses for the host gateway, so Xdebug reaches your IDE on any podman networking stack (rootless or rootful, pasta, netavark, slirp4netns).
:::

---

## Pre-built images

lerd ships pre-built PHP-FPM base images on ghcr.io for all supported versions (8.1–8.5), covering both `amd64` and `arm64`. When you run `lerd fetch` or `lerd php:rebuild`, lerd pulls the matching base image and layers just your mkcert CA certificate on top — bringing first-time build time from ~5 minutes down to ~30 seconds.

The base image tag is derived from the embedded Containerfile, so lerd always pulls the exact image that matches the version of lerd you have installed. If the pull fails (no internet, image not yet published) lerd falls back to a full local build transparently.

The images are public, so no ghcr.io login is required. lerd pulls them anonymously even if you are already logged into ghcr.io, to avoid authentication errors from expired or unrelated credentials.

`lerd start` checks all required images before starting containers. If any are missing (e.g. after `podman image rm`), it rebuilds or pulls them automatically using the same parallel spinner UI, so containers always start against a valid image.

To build entirely from source instead:

```bash
lerd fetch --local
lerd fetch --local 8.4
lerd php:rebuild --local
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

```bash
lerd php:ext list                # show custom extensions for current version
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

---

## PHP shell

`lerd shell` opens an interactive `sh` session inside the PHP-FPM container for the current project:

```bash
lerd shell
```

The PHP version is resolved the same way as every other lerd command (`.php-version`, `composer.json`, global default). The shell's working directory is set to the project root.

If the container is not running, lerd prints the `systemctl` command needed to start it rather than silently failing.

If the site is paused, any services referenced in `.env` (MySQL, Redis, etc.) are started automatically before the shell opens — the site itself stays paused.

---

### Composer.json detection

When you run `lerd park` or `lerd link`, Lerd reads `composer.json` and warns if any `ext-*` requirements are not covered by the bundled or installed extension set:

```
[!] my-app requires PHP extensions not in the image: swoole
    Run: lerd php:ext add swoole
```
