# Configuration

## Global config — `~/.config/lerd/config.yaml`

Created automatically on first run with sensible defaults:

```yaml
php:
  default_version: "8.5"
node:
  default_version: "22"
nginx:
  http_port: 80
  https_port: 443
dns:
  tld: "test"
parked_directories:
  - ~/Lerd
services:
  mysql:       { enabled: true,  image: "mysql:8.0",                    port: 3306 }
  redis:       { enabled: true,  image: "redis:7-alpine",               port: 6379 }
  postgres:    { enabled: false, image: "postgres:16-alpine",           port: 5432 }
  meilisearch: { enabled: false, image: "getmeili/meilisearch:v1.7",    port: 7700 }
  rustfs:      { enabled: false, image: "rustfs/rustfs:latest",         port: 9000 }
  mailpit:     { enabled: false, image: "axllent/mailpit:latest",       port: 1025 }
```

---

## Per-project config — `.lerd.yaml`

Optional file in a project root to override site settings. Created by `lerd init` or written manually:

```yaml
php_version: "8.4"
framework: laravel
secured: true
services:
  - mysql
  - redis
```

| Field | Description |
|---|---|
| `php_version` | PHP version for this project — highest priority, overrides `.php-version` and `composer.json` |
| `framework` | Framework name — overrides auto-detection |
| `secured` | When `true`, `lerd init` enables HTTPS on apply |
| `services` | Services to start when `lerd init` or `lerd setup` applies the config (e.g. on a fresh machine). Also used by `lerd env` — connection values for these services are written to `.env` even if the env file does not reference them yet |

Commit `.lerd.yaml` to the repository. On a new machine, running `lerd init` reads it and restores the full configuration — PHP version, HTTPS, and services — without any prompts.

`lerd isolate`, the UI PHP version selector, and the MCP `site_php` tool all keep `php_version` in sync with `.php-version` when this file exists.

`lerd secure`, `lerd unsecure`, the UI HTTPS toggle, and the MCP `secure`/`unsecure` tools keep the `secured` field in sync when this file exists.
