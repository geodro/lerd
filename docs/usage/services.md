# Services

## Built-in services

| Command | Description |
|---|---|
| `lerd service start <name>` | Start a service (auto-installs on first use) |
| `lerd service stop <name>` | Stop a service container |
| `lerd service restart <name>` | Restart a service container; refreshes the quadlet first so config edits land |
| `lerd service status <name>` | Show systemd unit status |
| `lerd service list` | All services with status, version, and an Update column |
| `lerd service update <name> [tag]` | Pull a newer image and restart; tag selects an explicit upgrade target |
| `lerd service migrate <name> <target-tag>` | SQL dump + restore for cross-version moves (mysql, postgres) |
| `lerd service rollback <name>` | Swap back to the previously-running image (toggles) |
| `lerd service pin <name>` | Pin a service so it is never auto-stopped |
| `lerd service unpin <name>` | Unpin a service so it can be auto-stopped when unused |
| `lerd service expose <name> <host:container>` | Publish an extra port on a built-in service |
| `lerd service expose <name> <host:container> --remove` | Remove a previously exposed port |

Available services: `mysql` (8.4 LTS), `redis` (7-alpine), `postgres` (16 with PostGIS), `meilisearch` (v1.42), `rustfs` (S3-compatible), `mailpit` (SMTP catcher).

Default services are defined as YAML presets with `default: true` in the lerd binary. Adding or replacing a default service is a YAML edit, not a code change. Each preset declares its own `update_strategy` (patch / minor / rolling), whether `track_latest` should auto-bump fresh installs to the current upstream, and whether `allow_major_upgrade` lets the cross-strategy upgrade button cross numeric majors. See [Service updates](service-updates.md) for the full update / upgrade / migrate / rollback flow.

`lerd service list` shows the version (derived from the image tag) and an Update column with green / amber / violet badges:

```
Service              Version    Status     Update
────────────────────────────────────────────────────────────
mailpit              latest     active
meilisearch          v1.42.1    active
mysql                v8.4.9     active
postgres             v16        active
redis                v7.4.8     active
rustfs               latest     active
```

The Web UI, the TUI, and `lerd status` display the same labels. Services pinned to rolling tags (`latest`, `main`) show the tag verbatim. Services where an update is available show `→ <new-tag>`; cross-strategy upgrades show `⇧ <new-tag>` in amber.

### Exposing extra ports on built-in services

Built-in services publish a fixed set of ports by default. Use `lerd service expose` to bind additional host ports without recompiling or replacing the service:

```bash
# Expose MySQL on an extra port (e.g. for a second GUI client using a different port)
lerd service expose mysql 13306:3306

# Remove the extra port
lerd service expose mysql --remove 13306:3306
```

Extra port mappings are persisted in `~/.config/lerd/config.yaml` under `services.<name>.extra_ports` and are applied automatically every time the service starts. If the service is already running when you run `expose`, it is restarted immediately to apply the change.

You can also edit `~/.config/lerd/config.yaml` directly:

```yaml
services:
  mysql:
    extra_ports:
      - "13306:3306"
```

Then apply with `lerd service restart mysql`.

---

## Service credentials

::: tip Two sets of hostnames
Services run as Podman containers on the `lerd` network. Two hostnames apply depending on where you're connecting from:

- **From host tools** (e.g. TablePlus, Redis CLI): use `127.0.0.1`
- **From your Laravel app** (PHP-FPM runs inside the `lerd` network): use container hostnames (e.g. `lerd-mysql`)

`lerd service start <name>` prints the correct `.env` variables to paste into your project.
:::

| Service | Default version | Host (host tools) | Host (Laravel `.env`) | Port | User | Password | DB |
|---|---|---|---|---|---|---|---|
| MySQL | 8.4 LTS (`mysql:8.4`) | 127.0.0.1 | lerd-mysql | 3306 | root | `lerd` | `lerd` |
| PostgreSQL | 16 + PostGIS 3.5 | 127.0.0.1 | lerd-postgres | 5432 | postgres | `lerd` | `lerd` |
| Redis | 7-alpine | 127.0.0.1 | lerd-redis | 6379 | - | - | - |
| Meilisearch | v1.42 | 127.0.0.1 | lerd-meilisearch | 7700 | - | - | - |
| RustFS | latest | 127.0.0.1 | lerd-rustfs | 9000 | `lerd` | `lerdpassword` | per-site bucket |
| Mailpit SMTP | latest | 127.0.0.1 | lerd-mailpit | 1025 | - | - | - |

Additional UIs:

- RustFS console: `http://127.0.0.1:9001`
- Mailpit web UI: `http://127.0.0.1:8025`

### RustFS, per-site buckets

RustFS is an S3-compatible object storage service (a drop-in replacement for MinIO). When `lerd env` detects it is needed (via `FILESYSTEM_DISK=s3` or `AWS_ENDPOINT` in `.env`), it automatically:

1. Creates a bucket named after the site handle, sanitised to match the S3 naming rules (lowercase, digits, hyphens, dots only, max 63 chars). Underscores in the handle are rewritten as hyphens, so `admin_astrolov` becomes bucket `admin-astrolov`.
2. Sets the bucket to **public access** (suitable for local development)
3. Writes the correct `.env` values:

```ini
FILESYSTEM_DISK=s3
AWS_ACCESS_KEY_ID=lerd
AWS_SECRET_ACCESS_KEY=lerdpassword
AWS_DEFAULT_REGION=us-east-1
AWS_BUCKET=my-project
AWS_URL=http://localhost:9000/my-project
AWS_ENDPOINT=http://lerd-rustfs:9000
AWS_USE_PATH_STYLE_ENDPOINT=true
```

If a historical `AWS_BUCKET` value with underscores (or other S3-invalid characters) is present from an earlier lerd run or Sail import, `lerd env` will sanitise it in place on the next run.

`AWS_URL` points to the public bucket URL (browser-reachable). `AWS_ENDPOINT` is the internal container address used by PHP.

### Migrating from MinIO to RustFS

RustFS exposes the same S3 API as MinIO with the same default credentials, no application changes are needed after migration.

**Automatic prompt during `lerd update`**

If lerd detects an existing MinIO data directory (`~/.local/share/lerd/data/minio`) during `lerd update`, it will offer to migrate automatically:

```
==> MinIO detected, migrate to RustFS? [y/N]
```

Answering `y` runs the full migration in-place. The update continues regardless of your answer.

**Manual migration**

```bash
lerd minio:migrate
```

This command:

1. Stops the `lerd-minio` container (if running)
2. Removes the MinIO quadlet so it no longer auto-starts
3. Copies `~/.local/share/lerd/data/minio/` to `~/.local/share/lerd/data/rustfs/`
4. Updates `~/.config/lerd/config.yaml`: removes the `minio` entry and adds `rustfs`
5. Installs and starts the `lerd-rustfs` service

The original MinIO data directory is **not deleted**. Verify the migration works, then remove it manually:

```bash
rm -rf ~/.local/share/lerd/data/minio
```

---

## More

- [Service updates](service-updates.md): the Update / Upgrade / Migrate / Rollback flow, `update_strategy` / `track_latest` / `allow_major_upgrade` configuration, and recovery from failed migrations.
- [Service presets](service-presets.md): one-command installers for phpMyAdmin, pgAdmin, MongoDB, alternate MySQL / MariaDB versions, Selenium, and Stripe Mock.
- [Custom services](custom-services.md): YAML schema for your own OCI-based services, with env injection, placeholders, dependencies, and worked examples (Soketi, Stripe).
