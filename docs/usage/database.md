# Database

Database shortcuts read `DB_CONNECTION`, `DB_DATABASE`, `DB_USERNAME`, and `DB_PASSWORD` from the project's `.env` and run the appropriate command inside the service container.

## Commands

| Command | Description |
|---|---|
| `lerd db:create [name]` | Create a database and a `<name>_testing` database for the current project |
| `lerd db:import [-d name] <file.sql>` | Import a SQL dump (defaults to site DB from `.env`) |
| `lerd db:export [-d name] [-o file.sql]` | Export a database to a SQL dump (defaults to site DB from `.env`) |
| `lerd db:shell` | Open an interactive MySQL or PostgreSQL shell for the current project |
| `lerd db create [name]` | Same as `db:create` (subcommand form) |
| `lerd db import [-d name] <file.sql>` | Same as `db:import` (subcommand form) |
| `lerd db export [-d name]` | Same as `db:export` (subcommand form) |
| `lerd db shell` | Same as `db:shell` (subcommand form) |

---

## `lerd db:create` name resolution

Name is resolved in this order (first match wins):

1. Explicit `[name]` argument
2. `DB_DATABASE` from the project's `.env`
3. Project name derived from the registered site name (or directory name)

A `<name>_testing` database is always created alongside the main one. If a database already exists the command reports it instead of failing.

Supports `DB_CONNECTION=mysql` / `mariadb` (via `lerd-mysql`) and `pgsql` / `postgres` (via `lerd-postgres`).

---

## Picking a database for a project

The database for a Laravel project is configured through `.lerd.yaml` and applied to `.env` when `lerd env` runs (which the `lerd init` wizard calls automatically). The supported choices are:

| Choice | Service | `.env` keys written |
|---|---|---|
| `sqlite` | none — local file | `DB_CONNECTION=sqlite`, `DB_DATABASE=database/database.sqlite` |
| `mysql` | `lerd-mysql` (Podman) | `DB_CONNECTION=mysql`, `DB_HOST=lerd-mysql`, `DB_PORT=3306`, `DB_DATABASE=<project>`, `DB_USERNAME=root`, `DB_PASSWORD=lerd` |
| `postgres` | `lerd-postgres` (Podman) | `DB_CONNECTION=pgsql`, `DB_HOST=lerd-postgres`, `DB_PORT=5432`, `DB_DATABASE=<project>`, `DB_USERNAME=postgres`, `DB_PASSWORD=lerd` |

Picking a database is exclusive — `.lerd.yaml` only ever lists one of `sqlite`, `mysql`, or `postgres`. Switching between them in the wizard immediately rewrites the relevant `.env` keys; the previous database's settings are not preserved.

For SQLite, the `database/database.sqlite` file is created automatically if it doesn't exist, and the `database/` directory is created alongside it. No service is started.

For MySQL or PostgreSQL, the matching `lerd-<engine>` service is started if it isn't already, and the project database (plus a `_testing` variant) is created via `lerd db:create`. The database name is inferred from the registered site name, or from `DB_DATABASE` in `.env` if it was set explicitly.

You can change the choice at any time by editing the `services:` list in `.lerd.yaml` (replace the current database entry with the desired one) and re-running `lerd env`, or by running `lerd init --fresh` and picking a different database in the wizard.
