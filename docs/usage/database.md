# Database

Database commands work with any project type — Laravel, Symfony, NestJS, Next.js, or any other framework. lerd automatically detects which database service to use through a resolution chain described below.

## Commands

| Command | Description |
|---|---|
| `lerd db:create [name]` | Create a database and a `<name>_testing` database |
| `lerd db:import [-s service] [-d name] <file.sql>` | Import a SQL dump |
| `lerd db:export [-s service] [-d name] [-o file.sql]` | Export a database to a SQL dump |
| `lerd db:shell [-s service] [-d name]` | Open an interactive MySQL or PostgreSQL shell |
| `lerd db create [name]` | Same as `db:create` (subcommand form) |
| `lerd db import [-s service] [-d name] <file.sql>` | Same as `db:import` (subcommand form) |
| `lerd db export [-s service] [-d name]` | Same as `db:export` (subcommand form) |
| `lerd db shell [-s service] [-d name]` | Same as `db:shell` (subcommand form) |

### Flags

| Flag | Short | Description |
|---|---|---|
| `--service <name>` | `-s` | Target a specific lerd service (e.g. `mysql`, `postgres`, `mysql-5-7`) |
| `--database <name>` | `-d` | Override the database name |
| `--output <file>` | `-o` | Output file for `db:export` (default: `<database>.sql`) |

---

## Service and database resolution

Every db command resolves which service to target and which database to use through the following chain (first match wins):

1. **`--service` flag** — explicit override, e.g. `lerd db:shell --service postgres`
2. **`.lerd.yaml` `db:` block** — declared in the project root, works even on unlinked sites
3. **Framework definition** — lerd detects the framework and uses its service detection rules against the framework's env file (e.g. `.env.local` for Symfony)
4. **`.env` key inference** — reads `DB_CONNECTION`, `DB_TYPE`, `TYPEORM_CONNECTION`, `DATABASE_URL`, or `DB_PORT` from `.env`
5. **Error** — with instructions listing all options above

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
| `DB_CONNECTION` | Laravel (`mysql`, `pgsql`, …) |
| `DB_TYPE` | TypeORM / NestJS (`postgres`, `mysql`, …) |
| `TYPEORM_CONNECTION` | TypeORM CLI |
| `DATABASE_URL` | Prisma, Drizzle, Symfony, Next.js (`postgresql://…`, `mysql://…`) |
| `DB_PORT` | Last resort — `5432` → postgres, `3306`/`3307` → mysql |

The database name is resolved from `DB_DATABASE`, `TYPEORM_DATABASE`, or the path component of `DATABASE_URL` (Prisma's `?schema=public` suffix is stripped automatically).

---

## `lerd db:create` name resolution

Name is resolved in this order (first match wins):

1. Explicit `[name]` argument
2. Database name from the resolution chain above
3. Project name derived from the registered site name (or directory name)

A `<name>_testing` database is always created alongside the main one. If a database already exists the command reports it instead of failing.

---

## Picking a database for a Laravel project

The database for a Laravel project is configured through `.lerd.yaml` and applied to `.env` when `lerd env` runs (which the `lerd init` wizard calls automatically). The supported choices are:

| Choice | Service | `.env` keys written |
|---|---|---|
| `sqlite` | none — local file | `DB_CONNECTION=sqlite`, `DB_DATABASE=database/database.sqlite` |
| `mysql` | `lerd-mysql` (Podman) | `DB_CONNECTION=mysql`, `DB_HOST=lerd-mysql`, `DB_PORT=3306`, `DB_DATABASE=<project>`, `DB_USERNAME=root`, `DB_PASSWORD=lerd` |
| `postgres` | `lerd-postgres` (Podman) | `DB_CONNECTION=pgsql`, `DB_HOST=lerd-postgres`, `DB_PORT=5432`, `DB_DATABASE=<project>`, `DB_USERNAME=postgres`, `DB_PASSWORD=lerd` |

For SQLite, the `database/database.sqlite` file is created automatically if it doesn't exist. No service is started.

For MySQL or PostgreSQL, the matching `lerd-<engine>` service is started if it isn't already, and the project database (plus a `_testing` variant) is created via `lerd db:create`.

You can change the choice at any time by editing the `services:` list in `.lerd.yaml` and re-running `lerd env`, or by running `lerd init --fresh` and picking a different database in the wizard.

---

## Non-PHP projects

For projects without a lerd framework definition (NestJS, Next.js, Go, etc.), db commands work without any lerd-specific configuration if the project's `.env` uses a recognised key:

```bash
# NestJS / TypeORM — DB_TYPE is sufficient
lerd db:shell

# Next.js / Prisma — DATABASE_URL is sufficient
lerd db:shell

# No .env at all — use --service
lerd db:shell --service postgres --database myapp

# Or declare it once in .lerd.yaml
# db:
#   service: postgres
#   database: myapp
lerd db:shell
```
