# Docker Migration

If you are switching from a Docker-based setup (Laravel Sail, Laradock, DDEV, or a custom `docker-compose.yml`), Lerd can detect potential conflicts and help you migrate your database data.

## Detecting conflicts

Run `lerd doctor` to see if Docker is interfering with Lerd. When real Docker is installed, a **Docker Compatibility** section appears automatically and checks for:

- Whether the Docker daemon is running (port and iptables conflicts)
- Running Docker containers that hold ports Lerd needs (80, 443, 3306, 5432, etc.)
- Docker iptables rules that may break rootless Podman networking
- Docker volumes that look like database data

If Docker is not installed (or only the harmless `podman-docker` shim is present), the section is hidden entirely.

## Common issues

### Port conflicts

Docker containers may bind the same host ports as Lerd services. The fix is simple — stop the Docker containers:

```bash
docker compose down          # in your project directory
# or
sudo systemctl stop docker   # stop the daemon entirely
```

`lerd install` will warn you if the Docker daemon is running and offer to continue anyway.

### iptables interference

The Docker daemon inserts iptables rules that can break rootless Podman networking. Stopping the Docker daemon clears these rules on reboot, or you can flush them immediately:

```bash
sudo systemctl stop docker
sudo iptables -F DOCKER
sudo iptables -X DOCKER
```

### Separate image stores

Docker and Podman use separate image storage. Images pulled via `docker pull` are not visible to Podman and vice versa. Lerd pulls its own images automatically — no action needed.

## Migrating databases

The `lerd docker:import` command exports databases from running Docker containers and imports them into Lerd's Podman-managed services.

### List available databases

```bash
lerd docker:import --list
```

Shows running Docker database containers and database volumes.

### Interactive migration

```bash
lerd docker:import
```

Walks you through selecting a Docker container, picking databases, and importing them. Credentials are auto-detected from the container's environment variables.

### Direct migration

```bash
lerd docker:import --source sail-mysql-1 --database laravel
lerd docker:import --source sail-mysql-1 --all-databases
```

### How it works

1. Reads the Docker container's environment to discover the root password
2. Runs `mysqldump` or `pg_dump` inside the Docker container via `docker exec`
3. Creates the database in the Lerd service if it doesn't exist
4. Pipes the dump into the Lerd service via `podman exec`

The export and import use `docker exec` and `podman exec` respectively, so there are no host-port conflicts even if both Docker and Lerd bind the same ports.

### Supported databases

| Docker image | Lerd service | Notes |
|---|---|---|
| `mysql:*` | `lerd-mysql` | Auto-detected |
| `mariadb:*` | `lerd-mysql` | Treated as MySQL-compatible |
| `postgres:*` | `lerd-postgres` | Auto-detected |

### After migration

Update your project's `.env` to point to Lerd services:

```bash
lerd env
```

Or set the values manually:

**MySQL:**
```
DB_HOST=lerd-mysql
DB_PORT=3306
DB_USERNAME=root
DB_PASSWORD=lerd
```

**PostgreSQL:**
```
DB_HOST=lerd-postgres
DB_PORT=5432
DB_USERNAME=postgres
DB_PASSWORD=lerd
```

## Recommended workflow

1. Run `lerd doctor` to see the current state
2. Run `lerd docker:import` to migrate databases
3. Stop Docker: `sudo systemctl stop docker && sudo systemctl disable docker`
4. Run `lerd start`
5. Run `lerd env` in each project to reconfigure `.env` files
