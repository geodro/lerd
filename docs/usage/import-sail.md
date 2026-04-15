# Importing from Laravel Sail

`lerd import sail` migrates a Laravel Sail project into lerd in one command — database and S3/MinIO storage included.

```bash
cd ~/Projects/myapp   # the Sail project root
lerd sail import
# or equivalently:
lerd import sail
```

::: tip Automatic prompt on `lerd link`
When you run `lerd link` on a project that has `laravel/sail` in `composer.json`, lerd automatically asks whether to run the import before setup — so you don't have to remember the command.
:::

---

## Prerequisites

- **Docker** (Docker Desktop, Docker Engine) **or Podman** with the `compose` plugin — whichever you used to run Sail
- The Sail project must have a `docker-compose.yml` in the current directory
- lerd must be running (`lerd start`)

---

## What it does

1. **Reads the compose file** and remaps any ports that conflict with lerd's running services (MySQL, Redis, MinIO/RustFS, …) so Sail can start alongside lerd without clashes
2. **Strips non-essential ports** from app / proxy services (nginx, traefik, Vite dev server, …) so only data services are exposed on the host
3. **Starts only the data services** (DB, and MinIO when S3 is needed) with `compose up -d --no-deps`, so Sail's app container is never built. A broken or slow app image build (Node/npm quirks, missing build args, private registry auth, …) doesn't block the import because lerd only needs the database and storage volumes.
4. **Waits for the database** to be ready, then auto-detects the correct database name (handles the case where `lerd env` has already overwritten `DB_DATABASE`)
5. **Dumps the database** from the Sail container and imports it into lerd's MySQL or PostgreSQL
6. **Mirrors S3/MinIO files** from the Sail MinIO container into lerd's RustFS (skipped automatically when S3 is not configured)
7. **Stops Sail** when the import is complete (unless `--no-stop` is passed)

---

## Example output

```
Stripping ports from app services: app, traefik, mailhog
Remapping conflicting ports for Sail:
  3306  → 23306
  6379  → 26379
  9000  → 29000
Starting Sail services: mysql, minio
Waiting for Sail mysql to be ready...
Dumping database "laravel" from Sail...
Importing into lerd (mysql / myapp)...
Database imported.
Importing S3/MinIO files into lerd RustFS...
`sail/local/avatars/abc.jpg` → `lerd/myapp/avatars/abc.jpg`
...
S3 import complete.

Import complete.
Stopping Sail...
```

---

## Flags

| Flag | Description |
|---|---|
| `--no-stop` | Leave Sail running after the import completes |
| `--skip-s3` | Skip S3/MinIO file mirroring |
| `--sail-db-name <name>` | Database name in the Sail environment (default: auto-detected) |
| `--sail-db-user <user>` | Database username in the Sail environment (default: `sail`) |
| `--sail-db-password <pass>` | Database password in the Sail environment (default: `password`) |

---

## Credential handling

### After `lerd env` has already run

`lerd env` overwrites `DB_*` and `AWS_*` keys with lerd's connection values. The import handles this transparently:

- **Database credentials**: The command tries Sail's default credentials (`sail` / `password`) and queries the MySQL container directly to auto-detect the database name — so `DB_DATABASE=recruitireland` in a lerd-overwritten `.env` is no obstacle when the actual Sail volume contains a database called `laravel`
- **MinIO credentials**: Read from the `MINIO_ROOT_USER` / `MINIO_ROOT_PASSWORD` environment keys in the `docker-compose.yml` service definition, not from `.env` — so lerd's `AWS_ACCESS_KEY_ID=lerd` never interferes

Use `--sail-db-*` flags only when your project uses non-default Sail credentials.

### `.env.before_lerd`

When `lerd env` first modifies a project's `.env` (before lerd has written any values to it), it saves the original file as `.env.before_lerd`. The import command reads S3 detection keys (`FILESYSTEM_DISK`, `AWS_ENDPOINT`, `AWS_BUCKET`) from this backup when it exists, so the original Sail S3 configuration is used even after lerd has overwritten `.env`.

---

## Restoring after import

If you want to switch back to Sail after importing:

```bash
lerd env:restore   # restores .env from .env.before_lerd
```

Then run `lerd env` again to re-apply lerd settings if you change your mind.

---

## Docker vs Podman

The command auto-detects which runtime is available:

1. Tries `docker compose version` — uses Docker if it works
2. Falls back to `podman compose version` — works for users running Sail with Podman Compose

No configuration needed.

---

## Troubleshooting

**`docker compose` / `podman compose` not found**

Install Docker Desktop, Podman Desktop, or the relevant compose plugin and ensure it is on your `PATH`.

**Database dump fails with access denied**

The Sail MySQL volume was created with different credentials. Pass the correct credentials:

```bash
lerd sail import --sail-db-user root --sail-db-password secret
```

**S3 import fails**

Run the import with `--skip-s3` and mirror the files manually using `mc`:

```bash
lerd sail import --skip-s3
```

**`lerd env` has already set `DB_DATABASE` to something else**

The command auto-detects the correct database from the container. If it picks the wrong one (multiple user databases in the same container), specify it explicitly:

```bash
lerd sail import --sail-db-name myapp
```
