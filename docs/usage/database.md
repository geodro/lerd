# Database

Database commands work with any project type: Laravel, Symfony, NestJS, Next.js, or any other framework. lerd automatically detects which database service to use through a resolution chain described below.

## Commands

| Command | Description |
|---|---|
| `lerd db:create [name]` | Create a database and a `<name>_testing` database |
| `lerd db:import [-s service] [-d name] <file.sql>` | Import a SQL dump |
| `lerd db:export [-s service] [-d name] [-o file.sql]` | Export a database to a SQL dump |
| `lerd db:shell [-s service] [-d name]` | Open an interactive MySQL or PostgreSQL shell |
| `lerd db:snapshot [name] [-A]` | Create a named, restorable snapshot of a database |
| `lerd db:snapshots [--all]` | List stored snapshots |
| `lerd db:restore <name> [-A] [-f]` | Restore a database from a stored snapshot |
| `lerd db:snapshot:rm <name> [-A]` | Delete a stored snapshot |
| `lerd db create [name]` | Same as `db:create` (subcommand form) |
| `lerd db import [-s service] [-d name] <file.sql>` | Same as `db:import` (subcommand form) |
| `lerd db export [-s service] [-d name]` | Same as `db:export` (subcommand form) |
| `lerd db shell [-s service] [-d name]` | Same as `db:shell` (subcommand form) |
| `lerd db snapshot [name]` | Same as `db:snapshot` (subcommand form) |
| `lerd db snapshots` | Same as `db:snapshots` (subcommand form) |
| `lerd db restore <name>` | Same as `db:restore` (subcommand form) |
| `lerd db snapshot:rm <name>` | Same as `db:snapshot:rm` (subcommand form) |

### Flags

| Flag | Short | Description |
|---|---|---|
| `--service <name>` | `-s` | Target a specific lerd service (e.g. `mysql`, `postgres`, `mysql-5-7`) |
| `--database <name>` | `-d` | Override the database name |
| `--output <file>` | `-o` | Output file for `db:export` (default: `<database>.sql`) |
| `--all-databases` | `-A` | Snapshot or restore every database in the service at once |
| `--force` | `-f` | Skip the `db:restore` confirmation prompt |
| `--all` | | List snapshots across every database on the service (`db:snapshots`) |

---

## Service and database resolution

Every db command resolves which service to target and which database to use through the following chain (first match wins):

1. **`--service` flag**: explicit override, e.g. `lerd db:shell --service postgres`
2. **`.lerd.yaml` `db:` block**: declared in the project root, works even on unlinked sites
3. **Framework definition**: lerd detects the framework and uses its service detection rules against the framework's env file (e.g. `.env.local` for Symfony)
4. **`.env` key inference**: reads `DB_CONNECTION`, `DB_TYPE`, `TYPEORM_CONNECTION`, `DATABASE_URL`, or `DB_PORT` from `.env`
5. **Error**: with instructions listing all options above

The `--database` flag overrides the database name at any resolution level.

### `.lerd.yaml` `db:` block

Add a `db:` block to `.lerd.yaml` to set a persistent default for the project. Useful for non-PHP projects that don't have a lerd framework definition.

```yaml
db:
  service: postgres
  database: myapp
```

### Supported `.env` keys

When falling back to `.env` inference, lerd checks the following keys in order to determine the database type:

| Key | Frameworks |
|---|---|
| `DB_CONNECTION` | Laravel (`mysql`, `pgsql`, etc.) |
| `DB_TYPE` | TypeORM / NestJS (`postgres`, `mysql`, etc.) |
| `TYPEORM_CONNECTION` | TypeORM CLI |
| `DATABASE_URL` | Prisma, Drizzle, Symfony, Next.js (`postgresql://...`, `mysql://...`) |
| `DB_PORT` | Last resort: `5432` for postgres, `3306`/`3307` for mysql |

The database name is resolved from `DB_DATABASE`, `TYPEORM_DATABASE`, or the path component of `DATABASE_URL` (Prisma's `?schema=public` suffix is stripped automatically).

---

## `lerd db:create` name resolution

Name is resolved in this order (first match wins):

1. Explicit `[name]` argument
2. Database name from the resolution chain above
3. Project name derived from the registered site name (or directory name)

A `<name>_testing` database is always created alongside the main one. If a database already exists the command reports it instead of failing.

---

## Snapshots

Snapshots are named, restorable point-in-time copies of a database, stored inside lerd's own data directory. Use one as a safety net before a risky migration, a branch switch, or any destructive experiment, then roll back in a single command. Snapshots cover the SQL engines only: MySQL, MariaDB, and PostgreSQL.

```bash
lerd db:snapshot pre-migration       # snapshot the current project database
lerd db:snapshot                     # name omitted: auto-named snapshot-<timestamp>
lerd db:snapshots                    # list snapshots for this database
lerd db:restore pre-migration        # restore it (prompts for confirmation)
lerd db:snapshot:rm pre-migration    # delete it
```

Snapshots live under `~/.local/share/lerd/snapshots/<service>/`, one directory per snapshot holding a gzipped SQL dump and a `meta.json` sidecar. They are scoped to a `(service, database)` pair, so two projects can both keep a snapshot called `pre-migration` without colliding. The same service-and-database resolution chain as every other db command applies, so from inside a project directory the snapshot commands just work.

### Restoring

`lerd db:restore <name>` is destructive. A per-database restore **drops and recreates** the target database before loading the dump, so the restore is clean with no leftover tables. It prompts for confirmation; pass `--force` to skip the prompt (required when running non-interactively, e.g. in a script).

### All databases

Pass `--all-databases` (`-A`) to snapshot or restore every database in the service at once instead of a single one:

```bash
lerd db:snapshot --service mysql --all-databases nightly
lerd db:restore --service mysql --all-databases nightly
```

An all-databases restore drops and recreates every database contained in the snapshot, but leaves databases that aren't in the snapshot untouched.

### Reserved names

`db:snapshot` rejects names that look like command verbs (`list`, `rm`, `delete`, `restore`, …), so `lerd db snapshot list` errors with a hint instead of silently creating a snapshot literally named "list". Use `lerd db:snapshots` to list.

---

## Picking a database for a Laravel project

The database for a Laravel project is configured through `.lerd.yaml` and applied to `.env` when `lerd env` runs (which the `lerd init` wizard calls automatically). The supported choices are:

| Choice | Service | `.env` keys written |
|---|---|---|
| `sqlite` | none (local file) | `DB_CONNECTION=sqlite`, `DB_DATABASE=database/database.sqlite` |
| `mysql` | `lerd-mysql` (Podman) | `DB_CONNECTION=mysql`, `DB_HOST=lerd-mysql`, `DB_PORT=3306`, `DB_DATABASE=<project>`, `DB_USERNAME=root`, `DB_PASSWORD=lerd` |
| `postgres` | `lerd-postgres` (Podman) | `DB_CONNECTION=pgsql`, `DB_HOST=lerd-postgres`, `DB_PORT=5432`, `DB_DATABASE=<project>`, `DB_USERNAME=postgres`, `DB_PASSWORD=lerd` |

Installed family alternates are valid picks too: `mariadb` / `mariadb-10-11`, `mysql-5-7`, `postgres-pgvector` / `postgres-17`, etc. They go through the same env-write + database-create flow as the built-ins, using the host and port from their preset. Install one first with `lerd service preset <name>`, then list it in `.lerd.yaml` under `services:` or pick it in the `lerd init` wizard.

For SQLite, the `database/database.sqlite` file is created automatically if it doesn't exist. No service is started.

For MySQL or PostgreSQL (and their family alternates), the matching `lerd-<service>` container is started if it isn't already, and the project database (plus a `_testing` variant) is created via `lerd db:create`.

You can change the choice at any time by editing the `services:` list in `.lerd.yaml` and re-running `lerd env`, or by running `lerd init --fresh` and picking a different database in the wizard.

---

## Non-PHP projects

For projects without a lerd framework definition (NestJS, Next.js, Go, etc.), db commands work without any lerd-specific configuration if the project's `.env` uses a recognised key:

```bash
# NestJS / TypeORM, DB_TYPE is sufficient
lerd db:shell

# Next.js / Prisma, DATABASE_URL is sufficient
lerd db:shell

# No .env at all, use --service
lerd db:shell --service postgres --database myapp

# Or declare it once in .lerd.yaml
# db:
#   service: postgres
#   database: myapp
lerd db:shell
```

## Recovering after a service reinstall

`lerd service reinstall <name> --reset-data` wipes the database server's data dir (rename-aside, recoverable) and then walks every active site that depends on the service to recreate the database it expects via `CREATE DATABASE IF NOT EXISTS`. Database name resolution is the same as `lerd env`: `.lerd.yaml` `db.database` first, then `.env` `DB_DATABASE`, then a name derived from the site name.

The DBs come back empty. The previous data lives next door as `~/.local/share/lerd/data/<name>.pre-remove-<timestamp>`. If you need the old contents, stop the service, rename the aside dir back over the new data dir, and start the service again.

If you only want to recreate a single missing database without wiping the whole server, use `lerd db:create` against the live service instead.
