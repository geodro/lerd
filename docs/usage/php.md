# PHP

## Commands

| Command | Description |
|---|---|
| `lerd use <version>` | Set the global PHP version and build the FPM image if needed |
| `lerd isolate <version>` | Pin PHP version for cwd — writes `.php-version` |
| `lerd php:list` | List all installed PHP-FPM versions |
| `lerd php:rebuild` | Force-rebuild all installed PHP-FPM images (run after `lerd update` if needed) |
| `lerd fetch [version...]` | Pre-build PHP FPM images for the given (or all supported) versions so first use isn't slow |
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

---

## Version resolution

When serving a request, Lerd picks the PHP version for a project in this order:

1. `.lerd.yaml` in the project root — `php_version` field (explicit lerd override)
2. `.php-version` file in the project root (plain text, e.g. `8.2`)
3. `composer.json` — `require.php` constraint (e.g. `^8.4` → `8.4`)
4. Global default in `~/.config/lerd/config.yaml`

When `.php-version` changes on disk, the lerd watcher automatically updates the site registry and regenerates the nginx vhost — no manual reload needed.

To pin a project permanently:

```bash
cd ~/Lerd/my-app
lerd isolate 8.2
# writes .php-version: 8.2 — commit this if you like
```

To change the global default:

```bash
lerd use 8.4
```

---

## Xdebug

??? info "Xdebug configuration values"
    Xdebug is configured with:

    - `xdebug.mode=debug`
    - `xdebug.start_with_request=yes`
    - `xdebug.client_host=host.containers.internal` (reaches your host IDE from the container)
    - `xdebug.client_port=9003`

    Set your IDE to listen on port `9003`. In VS Code, the default PHP Debug configuration works without changes. In PhpStorm, set **Settings → PHP → Debug → Debug port** to `9003`.

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
