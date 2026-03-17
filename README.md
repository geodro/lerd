# Lerd

Laravel Herd for Linux — a Podman-native local development environment for Laravel projects.

Lerd bundles Nginx, PHP-FPM, and optional services (MySQL, Redis, PostgreSQL, Meilisearch, MinIO) as rootless Podman containers, giving you automatic `.test` domain routing, per-project PHP/Node version isolation, and one-command TLS — all without touching your system's PHP or web server.

---

## Lerd vs Laravel Sail

[Laravel Sail](https://laravel.com/docs/sail) is the official per-project Docker Compose solution. Lerd is a shared infrastructure approach, closer to what [Laravel Herd](https://herd.laravel.com/) does on macOS. Both are valid — they solve slightly different problems.

| | Lerd | Laravel Sail |
|---|---|---|
| Nginx | One shared container for all sites | Per-project |
| PHP-FPM | One container per PHP version, shared | Per-project container |
| Services (MySQL, Redis…) | One shared instance | Per-project (or manually shared) |
| `.test` domains | Automatic, zero config | Manual `/etc/hosts` or dnsmasq |
| HTTPS | `lerd secure` → trusted cert instantly | Manual or roll your own mkcert |
| RAM with 5 projects running | ~200 MB | ~1–2 GB (5× stacks) |
| Requires changes to project files | No | Yes — needs `docker-compose.yml` committed |
| Works on legacy / client repos | Yes — just `lerd link` | Only if you can add Sail |
| Defined in code (infra-as-code) | No | Yes |
| Team parity (all OS) | Linux only | macOS, Windows, Linux |

**Choose Sail when:** your team uses it, you need per-project service versions, or you want infrastructure defined in the repo.

**Choose Lerd when:** you work across many projects at once and don't want a separate stack per repo, you can't modify project files, you want instant `.test` routing, or you're on Linux and want the Herd experience.

---

## Requirements

- Linux (Arch, Debian/Ubuntu, or Fedora-based)
- [Podman](https://podman.io/) (rootless, with systemd user session active)
- [NetworkManager](https://networkmanager.dev/) (for `.test` DNS)
- `systemctl --user` functional (`loginctl enable-linger $USER` if needed)
- `unzip` (used during install to extract fnm)

Go is only needed to build from source. The released binary has no runtime dependencies.

---

## Installation

### One-line installer (recommended)

With curl:

```bash
curl -fsSL https://raw.githubusercontent.com/geodro/lerd/main/install.sh | bash
```

With wget:

```bash
wget -qO- https://raw.githubusercontent.com/geodro/lerd/main/install.sh | bash
```

This will:
- Check and offer to install missing prerequisites (Podman, NetworkManager, unzip)
- Download the latest `lerd` binary for your architecture (amd64 / arm64)
- Install it to `~/.local/bin/lerd`
- Add `~/.local/bin` to your shell's `PATH` (bash, zsh, or fish)
- Automatically run `lerd install` to complete environment setup

> **DNS setup:** `lerd install` writes to `/etc/NetworkManager/dnsmasq.d/` and `/etc/NetworkManager/conf.d/` and restarts NetworkManager. This is the only step that requires `sudo`.

After install, reload your shell or open a new terminal so `PATH` takes effect.

### Install from a local build

If you built from source and want to skip the GitHub download:

```bash
make build
bash install.sh --local ./build/lerd
```

### Update

```bash
lerd update
```

Fetches the latest release from GitHub, downloads the binary for your architecture, and atomically replaces the running binary. No restart needed.

You can also re-run the installer:

```bash
curl -fsSL https://raw.githubusercontent.com/geodro/lerd/main/install.sh | bash -s -- --update
```

```bash
wget -qO- https://raw.githubusercontent.com/geodro/lerd/main/install.sh | bash -s -- --update
```

### Uninstall

```bash
lerd uninstall
```

Stops all containers, disables and removes Quadlet units, removes the watcher service, removes the binary, and cleans up the `PATH` entry from your shell config. Prompts before deleting config and data directories.

To skip all prompts:

```bash
lerd uninstall --force
```

### Check prerequisites only

```bash
bash install.sh --check
```

### From source

```bash
git clone https://github.com/geodro/lerd
cd lerd
make build
make install            # installs to ~/.local/bin/lerd
make install-installer  # installs lerd-installer to ~/.local/bin/
```

`lerd install` will:

1. Create XDG config and data directories
2. Create the `lerd` Podman network
3. Download static binaries: Composer, fnm, mkcert
4. Install the mkcert CA into your system trust store
5. Write and start the `lerd-dns` and `lerd-nginx` Podman Quadlet containers
6. Enable the `lerd-watcher` background service (auto-discovers new projects)
7. Add `~/.local/share/lerd/bin` to your shell's `PATH`

---

## Quick start

```bash
# 1. Park your projects directory — any Laravel project inside is auto-registered
lerd park ~/Lerd

# 2. Visit your project in a browser
#    ~/Lerd/my-app  →  http://my-app.test

# 3. Check everything is running
lerd status
```

That's it. Nginx is serving your project through PHP-FPM, all inside Podman containers on the `lerd` network.

---

## Commands

### Setup & lifecycle

| Command | Description |
|---|---|
| `lerd install` | One-time setup: directories, network, binaries, DNS, nginx, watcher |
| `lerd start` | Start DNS, nginx, PHP-FPM containers, and all installed services |
| `lerd stop` | Stop DNS, nginx, PHP-FPM containers, and all running services |
| `lerd update` | Update to the latest release |
| `lerd uninstall` | Stop all containers and remove Lerd |
| `lerd uninstall --force` | Same, skipping all confirmation prompts |
| `lerd dns:check` | Verify that `*.test` resolves to `127.0.0.1` |
| `lerd status` | Health summary: DNS, nginx, PHP-FPM containers, services, cert expiry |
| `lerd logs [-f] [target]` | Show logs for the current project's FPM container, `nginx`, a service name, or a PHP version |
| `lerd.test` | Browser dashboard — sites, services, system health, update button |

### Site management

| Command | Description |
|---|---|
| `lerd park [dir]` | Register all Laravel projects inside `dir` (defaults to cwd) |
| `lerd unpark [dir]` | Remove a parked directory and unlink all its sites |
| `lerd link [name]` | Register the current directory as a site |
| `lerd link [name] --domain foo.test` | Register with a custom domain |
| `lerd unlink` | Remove the current directory from Lerd |
| `lerd sites` | Table view of all registered sites |
| `lerd secure [name]` | Issue a mkcert TLS cert and enable HTTPS for a site |

> **Domain naming:** directories with real TLDs are automatically normalised — dots are replaced with dashes and the TLD is stripped before appending `.test`. For example `admin.astrolov.com` → `admin-astrolov.test`.

### PHP

| Command | Description |
|---|---|
| `lerd use <version>` | Set the global PHP version and build the FPM image if needed |
| `lerd isolate <version>` | Pin PHP version for cwd — writes `.php-version` |
| `lerd php:list` | List all installed PHP-FPM versions |
| `lerd php:rebuild` | Force-rebuild all installed PHP-FPM images (run after `lerd update` if needed) |
| `lerd php [args...]` | Run PHP in the project's container |
| `lerd artisan [args...]` | Run `php artisan` in the project's container |

### Node

| Command | Description |
|---|---|
| `lerd isolate:node <version>` | Pin Node version for cwd — writes `.node-version`, runs `fnm install` |
| `lerd node [args...]` | Run node using the project's version via fnm |
| `lerd npm [args...]` | Run npm using the project's version via fnm |
| `lerd npx [args...]` | Run npx using the project's version via fnm |

### Services

| Command | Description |
|---|---|
| `lerd service start <name>` | Start a service (auto-installs on first use) |
| `lerd service stop <name>` | Stop a service container |
| `lerd service restart <name>` | Restart a service container |
| `lerd service status <name>` | Show systemd unit status |
| `lerd service list` | Show all services and their current state |

Available services: `mysql`, `redis`, `postgres`, `meilisearch`, `minio`, `mailpit`, `soketi`.

### Shell completion

```bash
lerd completion bash   # add to ~/.bashrc
lerd completion zsh    # add to ~/.zshrc
lerd completion fish   # add to ~/.config/fish/completions/lerd.fish
```

---

## PHP version resolution

When serving a request, Lerd picks the PHP version for a project in this order:

1. `.php-version` file in the project root (plain text, e.g. `8.2`)
2. `.lerd.yaml` in the project root — `php_version` field
3. `composer.json` — `require.php` constraint (e.g. `^8.2` → `8.2`)
4. Global default in `~/.config/lerd/config.yaml`

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

## Node version resolution

1. `.nvmrc` in the project root
2. `.node-version` in the project root
3. `package.json` — `engines.node` field
4. Global default in `~/.config/lerd/config.yaml`

To pin a project:

```bash
cd ~/Lerd/my-app
lerd isolate:node 20
# writes .node-version and runs: fnm install 20
```

---

## HTTPS / TLS

Lerd uses [mkcert](https://github.com/FiloSottile/mkcert) — a locally-trusted CA that your browser will accept without warnings.

```bash
cd ~/Lerd/my-app
lerd secure
# Issues a cert for my-app.test, regenerates the SSL vhost, reloads nginx
# Visit https://my-app.test — no certificate warning
```

Certificates are stored in `~/.local/share/lerd/certs/sites/`.

---

## Configuration

### Global config — `~/.config/lerd/config.yaml`

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
  soketi:      { enabled: false, image: "quay.io/soketi/soketi:latest-16-alpine", port: 6001 }
```

### Per-project config — `.lerd.yaml`

Optional file in a project root to override site settings:

```yaml
php_version: "8.2"
node_version: "18"
domain: "my-app.test"   # override the auto-generated domain
secure: true
```

---

## Directory layout

```
~/.config/lerd/
└── config.yaml

~/.config/containers/systemd/        # Podman Quadlet units (auto-loaded)
~/.config/systemd/user/
└── lerd-watcher.service

~/.local/share/lerd/
├── bin/                             # mkcert, fnm, static PHP binaries
├── nginx/
│   ├── nginx.conf
│   ├── conf.d/                      # one .conf per site (auto-generated)
│   └── logs/
├── certs/
│   ├── ca/
│   └── sites/                       # per-domain .crt + .key
├── data/                            # Podman volume bind-mounts
│   ├── mysql/
│   ├── redis/
│   ├── postgres/
│   ├── meilisearch/
│   └── minio/
├── dnsmasq/
│   └── lerd.conf
└── sites.yaml
```

---

## Architecture

All containers join the rootless Podman network `lerd`. Communication between Nginx and PHP-FPM uses container names as hostnames:

```
Browser → 127.0.0.1:80 → lerd-nginx
                              └─ fastcgi_pass lerd-php84-fpm:9000
                                     └─ lerd-php84-fpm (mounts ~/Lerd read-only)

*.test → DNS → 127.0.0.1
                   └─ lerd-dns (dnsmasq, host network, port 5300)
                        ← NetworkManager forwards .test queries here
```

| Component | Technology |
|---|---|
| CLI | Go + Cobra, single static binary |
| Web server | Podman Quadlet — `nginx:alpine` |
| PHP-FPM | Podman Quadlet per version — locally built image with all Laravel extensions |
| PHP CLI | `php` binary inside the FPM container (`podman exec`) |
| Composer | `composer.phar` via bundled PHP CLI |
| Node | [fnm](https://github.com/Schniz/fnm) binary, version per project |
| Services | Podman Quadlet containers |
| DNS | dnsmasq container + NetworkManager integration |
| TLS | [mkcert](https://github.com/FiloSottile/mkcert) — locally trusted CA |

---

## Building

```bash
make build      # → ./build/lerd
make install    # → ~/.local/bin/lerd
make test       # go test ./...
make clean      # remove ./build/
```

Cross-compile for arm64:

```bash
GOARCH=arm64 GOOS=linux go build -o ./build/lerd-arm64 ./cmd/lerd
```

---

## Service credentials (defaults)

Services run as Podman containers on the `lerd` network. Two sets of hostnames apply:

- **From host tools** (e.g. TablePlus, Redis CLI): use `127.0.0.1`
- **From your Laravel app** (PHP-FPM runs inside the `lerd` network): use container hostnames (e.g. `lerd-mysql`)

`lerd service start <name>` prints the correct `.env` variables to paste into your project.

| Service | Host (host tools) | Host (Laravel `.env`) | Port | User | Password | DB |
|---|---|---|---|---|---|---|
| MySQL | 127.0.0.1 | lerd-mysql | 3306 | root | `lerd` | `lerd` |
| PostgreSQL | 127.0.0.1 | lerd-postgres | 5432 | postgres | `lerd` | `lerd` |
| Redis | 127.0.0.1 | lerd-redis | 6379 | — | — | — |
| Meilisearch | 127.0.0.1 | lerd-meilisearch | 7700 | — | — | — |
| MinIO | 127.0.0.1 | lerd-minio | 9000 | `lerd` | `lerdpassword` | — |
| Mailpit SMTP | 127.0.0.1 | lerd-mailpit | 1025 | — | — | — |
| Soketi | 127.0.0.1 | lerd-soketi | 6001 | — | — | — |

MinIO console is available at `http://127.0.0.1:9001`.

Mailpit web UI is available at `http://127.0.0.1:8025`.

Soketi metrics are available at `http://127.0.0.1:9601`.

---

## Troubleshooting

**`.test` domains not resolving**

```bash
lerd dns:check
# If it fails:
sudo systemctl restart NetworkManager
lerd dns:check
```

**Nginx not serving a site**

```bash
lerd status                         # check nginx and FPM are running
podman logs lerd-nginx              # nginx error log
cat ~/.local/share/lerd/nginx/conf.d/my-app.test.conf   # check generated vhost
```

**PHP-FPM container not running**

```bash
systemctl --user status lerd-php84-fpm
systemctl --user start lerd-php84-fpm
podman logs lerd-php84-fpm
```

**Permission denied on port 80/443**

Rootless Podman cannot bind to ports below 1024 by default. Allow it:

```bash
sudo sysctl -w net.ipv4.ip_unprivileged_port_start=80
# Make permanent:
echo 'net.ipv4.ip_unprivileged_port_start=80' | sudo tee /etc/sysctl.d/99-lerd.conf
```

**Watcher service not running**

```bash
systemctl --user status lerd-watcher
systemctl --user start lerd-watcher
```
