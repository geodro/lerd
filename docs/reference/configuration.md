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
  minio:       { enabled: false, image: "minio/minio:latest",           port: 9000 }
  mailpit:     { enabled: false, image: "axllent/mailpit:latest",       port: 1025 }
```

---

## Per-project config — `.lerd.yaml`

Optional file in a project root to override site settings:

```yaml
php_version: "8.2"
node_version: "18"
domain: "my-app.test"   # override the auto-generated domain
secure: true
```

The `.lerd.yaml` `php_version` field takes top priority in version resolution — it overrides both `composer.json` and `.php-version`.
