# WordPress walkthrough

End-to-end: from `lerd install` to a WordPress site running on `https://myblog.test` with MySQL.

::: info Prerequisites
You've already run `lerd install` once on this machine. If not, see [Installation](installation.md).
:::

::: tip Drive it from your AI assistant
Run `lerd mcp:enable-global` once and your AI assistant (Claude Code, Junie, Windsurf) can call every command below as an MCP tool: `site_link`, `db_create`, `secure`, etc. See [AI Integration](../features/mcp.md).
:::

---

## 1. Register the WordPress framework definition (one-time)

Save this as `~/.config/lerd/frameworks/wordpress.yaml`:

```yaml
# ~/.config/lerd/frameworks/wordpress.yaml
name: wordpress
label: WordPress
detect:
  - file: wp-login.php
  - file: wp-config.php
public_dir: .
env:
  fallback_file: wp-config.php
  fallback_format: php-const
composer: false
npm: false
```

Then register it:

```bash
lerd framework add wordpress --from-file ~/.config/lerd/frameworks/wordpress.yaml
```

::: info Why no `.env`?
WordPress stores configuration in `wp-config.php` as PHP constants, not in a `.env` file. The `fallback_file` / `fallback_format` settings tell lerd to read constants like `DB_HOST`, `WP_HOME`, and `WP_SITEURL` directly from `wp-config.php`. This means `lerd env` doesn't auto-inject database credentials the way it does for Laravel or Symfony; you'll wire them up by hand in step 5.
:::

---

## 2. Download WordPress

::: code-group

```bash [wp-cli]
cd ~/Lerd
wp core download --path=myblog
```

```bash [curl + tar]
cd ~/Lerd
mkdir myblog && cd myblog
curl -O https://wordpress.org/latest.tar.gz
tar -xzf latest.tar.gz --strip-components=1
rm latest.tar.gz
```

:::

---

## 3. Register the site

```bash
cd ~/Lerd/myblog
lerd link
```

`lerd link` detects WordPress (via `wp-login.php` or `wp-config.php`), assigns `http://myblog.test`, and serves from the project root.

---

## 4. Configure PHP and start MySQL

```bash
lerd init
```

```
? PHP version: 8.3
? Node version (leave blank to skip):
? Enable HTTPS? Yes
? Services: [mysql]
Saved .lerd.yaml
```

Workers are not shown; the WordPress framework definition declares none.

---

## 5. Create the database

```bash
lerd db:create myblog
```

This creates `myblog` and `myblog_testing` inside the lerd-mysql container.

::: info Database credentials
| Setting | Value |
|---|---|
| Host | `lerd-mysql` |
| Port | `3306` |
| User | `root` |
| Password | `lerd` |
| Database | `myblog` |

These come from the lerd built-in MySQL service. See [Services](../usage/services.md#service-credentials).
:::

---

## 6. Configure `wp-config.php`

Run the WordPress installer (browser at `http://myblog.test`) which will prompt for the values above, **or** copy `wp-config-sample.php` and edit it manually:

```bash
cp wp-config-sample.php wp-config.php
```

Then edit the `DB_*` constants:

```php
define( 'DB_NAME',     'myblog' );
define( 'DB_USER',     'root' );
define( 'DB_PASSWORD', 'lerd' );
define( 'DB_HOST',     'lerd-mysql' );
```

Generate fresh authentication salts (the installer does this automatically; for the manual path, replace the placeholder block with output from <https://api.wordpress.org/secret-key/1.1/salt/>).

---

## 7. Enable HTTPS

```bash
lerd secure myblog
```

This issues a trusted local cert via mkcert and switches the vhost to HTTPS. WordPress also stores its canonical URL in two places, so update them too:

```php
// wp-config.php
define( 'WP_HOME',    'https://myblog.test' );
define( 'WP_SITEURL', 'https://myblog.test' );
```

(Or update the same values in **Settings > General** from the WordPress admin.)

---

## 8. Open it

```bash
lerd open
```

Walk through the five-minute install (admin user, site title, password). When you're done, `https://myblog.test/wp-admin` is your dashboard.

---

## 9. Verify

```bash
lerd status
```

`myblog` should be listed as `active` and `mysql` as `running`. Live nginx and PHP-FPM logs are in the [Web UI](../features/web-ui.md) at `http://127.0.0.1:7073`.

---

## What just happened

| Command | What it did |
|---|---|
| `lerd framework add wordpress` | Registered the YAML so WordPress projects are auto-detected |
| `lerd link` | Assigned `myblog.test`, set document root to project root |
| `lerd init` | Wrote `.lerd.yaml` with PHP 8.3 and the MySQL service |
| `lerd db:create myblog` | Created `myblog` and `myblog_testing` inside lerd-mysql |
| (manual) `wp-config.php` edits | Pointed WordPress at `lerd-mysql` and the new database |
| `lerd secure myblog` | Issued mkcert TLS, switched vhost to HTTPS |

---

## Next steps

- [Frameworks & Workers](../usage/frameworks.md): extend `wordpress.yaml` to add log paths or custom workers (e.g. `wp cron event run`)
- [Database](../usage/database.md): `lerd db:import` to load a production dump, `lerd db:shell` for quick queries
- [Services](../usage/services.md): add a Mailpit service to capture outgoing mail in dev
- [HTTPS](../features/https.md): wildcard certs for multi-site or git worktrees
- [AI Integration (MCP)](../features/mcp.md): drive lerd from Claude Code, Cursor, Junie, etc.
