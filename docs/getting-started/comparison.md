# Lerd vs Other Tools

## Lerd vs Laravel Herd

[Laravel Herd](https://herd.laravel.com/) is the closest match in spirit: a zero-config shared stack with automatic `.test` domains, one-click HTTPS, and per-site PHP versions. Lerd is the open-source equivalent for Linux (and macOS), built on rootless Podman instead of native binaries.

|  | Lerd | Laravel Herd |
|---|---|---|
| Platforms | Linux, macOS | macOS, Windows (WSL2) |
| License | Open source (MIT) | Proprietary, freemium (Herd Pro subscription) |
| PHP runtime | Rootless Podman containers | Native binaries on macOS, WSL on Windows |
| PHP versions | 8.1, 8.2, 8.3, 8.4, 8.5 (shared FPM containers) | 7.4 through 8.5 (native) |
| `.test` domains | Automatic via dnsmasq container | Automatic via native dnsmasq resolver |
| HTTPS | `lerd secure` + mkcert, trusted system-wide | Built-in "Secure Site" toggle |
| Xdebug | `lerd xdebug:on`, tray toggle | Per-site toggle (Herd Pro) |
| Services | Built-in and free: MySQL, Postgres, Redis, Meilisearch, RustFS (S3), Mailpit; custom services via YAML presets | Some in the free tier; most advanced services and UIs (database inspector, log viewer, dumps) gated behind Herd Pro |
| Non-PHP projects | First-class via `Containerfile.lerd` (Node, Python, Go, Rails, ...) | Not directly supported, Herd focuses on PHP |
| Per-project config | `.lerd.yaml` committed to the repo; covers PHP, Node, framework, services, workers, and custom containers; applied by `lerd link` / `lerd init` | [`herd.yml`](https://herd.laravel.com/docs/macos/sites/herd-yaml) committed to the repo; covers name, PHP, TLS, aliases on the free tier; services require Herd Pro; applied by `herd init` |
| Queue + scheduler workers | `lerd worker start queue` / `schedule` as systemd user services | Queue UI (Herd Pro) |
| Dashboard | Web UI at `127.0.0.1:7073`, system tray, and an installable PWA at `lerd.localhost` | Native desktop app |
| Cost | Free | Free tier + paid Pro subscription |

**Choose Herd when:** you work on macOS or Windows, want the performance of native PHP binaries (no container overhead), are already in the Laravel Forge and Envoyer ecosystem, or are happy paying for Pro features.

**Choose Lerd when:** you work on Linux (Herd has no Linux build), prefer open source, want reproducible project setup checked into git via `.lerd.yaml`, need first-class non-PHP containers, or want a single tool that covers PHP, Node, Python, and Go behind the same `.test` routing.

### The "Herd on Linux" pitch

If you're moving from macOS + Herd to Linux, or want the same workflow on both, Lerd covers the Herd day-to-day surface:

- Automatic `.test` domains with no `/etc/hosts` edits
- Instant HTTPS via mkcert
- Per-site PHP version selection
- Shared MySQL, Postgres, Redis, Mailpit, S3-compatible storage
- Per-site queue workers and scheduler as systemd services
- A visual dashboard for sites, services, and logs

You trade a sliver of native performance (Podman Machine on macOS adds overhead versus native binaries) for rootless isolation, a fully open stack, reproducible `.lerd.yaml` project setup, and the same tool on Linux.

---

## Lerd vs Laravel Sail

[Laravel Sail](https://laravel.com/docs/sail) is the official per-project Docker Compose solution. Lerd is a shared infrastructure approach, closer to what [Laravel Herd](https://herd.laravel.com/) does on macOS. Both are valid; they solve slightly different problems.

|  | Lerd | Laravel Sail |
|---|---|---|
| Platforms | Linux, macOS | Linux, macOS, Windows |
| License | Open source (MIT) | Open source (MIT) |
| Container runtime | Rootless Podman | Docker Desktop / Orbstack / Colima |
| Architecture | One shared nginx + PHP-FPM + services across every site | Per-project Docker Compose stack |
| PHP versions | 8.1, 8.2, 8.3, 8.4, 8.5 (shared FPM containers) | Per-project Sail image (8.2, 8.3, 8.4) |
| Services (MySQL, Redis…) | One shared instance | Per-project (or manually shared) |
| `.test` domains | Automatic, zero config | Manual `/etc/hosts` or `localhost:${APP_PORT}` per project |
| HTTPS | `lerd secure` for trusted mkcert cert instantly | Manual or roll your own mkcert |
| Non-PHP projects | First-class via `Containerfile.lerd` (Node, Python, Go, Rails) | Add your own container to the Compose stack |
| Per-project config | `.lerd.yaml` (PHP + Node + services + workers) | `docker-compose.yml` + `Dockerfile` |
| RAM with 5 projects running | ~200 MB | ~1–2 GB (5× full stacks) |
| Requires changes to project files | No | Yes, needs `docker-compose.yml` committed |
| Works on legacy / client repos | Yes, just `lerd link` | Only if you can add Sail |
| Dashboard | Web UI at `127.0.0.1:7073`, system tray, installable PWA at `lerd.localhost` | CLI + Docker Desktop |
| Cost | Free | Free |

**Choose Sail when:** your team uses it, you need per-project service versions, or you want infrastructure defined in the repo.

**Choose Lerd when:** you work across many projects at once and don't want a separate stack per repo, you can't modify project files, you want instant `.test` routing, or you want the Herd experience on Linux as well as macOS.

### Migrating from Sail to Lerd

`lerd import sail` imports a Sail project's database and S3/MinIO files into lerd in one command, no manual dump/restore needed. It starts Sail temporarily with remapped ports (so there are no conflicts with lerd's running services), dumps the database, mirrors storage files, then tears Sail back down.

```bash
cd ~/Projects/myapp
lerd sail import
```

See [Importing from Laravel Sail](/usage/import-sail) for details.

---

## Lerd vs ddev

[ddev](https://ddev.com/) is a popular open-source local development tool that spins up per-project Docker containers with a shared Traefik router. It supports many frameworks (Laravel, WordPress, Drupal, etc.) and runs on macOS, Windows, and Linux. Lerd is narrower in scope (Laravel-focused, Podman-native, shared infrastructure) and closer to the Herd model.

|  | Lerd | ddev |
|---|---|---|
| Platforms | Linux, macOS | Linux, macOS, Windows |
| License | Open source (MIT) | Open source (Apache 2.0) |
| Container runtime | Rootless Podman | Docker / Orbstack / Colima |
| Architecture | Shared nginx + PHP-FPM across all projects | Per-project containers behind a shared reverse proxy |
| PHP versions | 8.1, 8.2, 8.3, 8.4, 8.5 (shared FPM containers) | 5.6 through 8.4+ (per-project Docker image) |
| Services (MySQL, Redis…) | One shared instance | Per-project (isolated by default) |
| Domains | `.test`, automatic | `.ddev.site` or custom, automatic via the built-in proxy |
| HTTPS | `lerd secure` for trusted mkcert cert instantly | Built-in via mkcert |
| Non-PHP projects | First-class via `Containerfile.lerd` (Node, Python, Go, Rails) | `nodejs` and `generic` project types |
| Per-project config | `.lerd.yaml` committed to the repo | `.ddev/config.yaml` committed to the repo |
| RAM with 5 projects running | ~200 MB | ~500 MB–1 GB (5× app containers + proxy) |
| Requires changes to project files | No | Yes, needs `.ddev/config.yaml` committed |
| Works on legacy / client repos | Yes, just `lerd link` | Only if you can add ddev config |
| Framework support | Laravel built-in; any PHP framework via YAML definitions | Laravel, WordPress, Drupal, TYPO3, and many more |
| Dashboard | Web UI at `127.0.0.1:7073`, system tray, installable PWA at `lerd.localhost` | CLI + optional per-project web UI |
| Cost | Free | Free |

**Choose ddev when:** your team is cross-platform, you work with multiple frameworks (not just Laravel), you want per-project service isolation, or your workflow already depends on Docker.

**Choose Lerd when:** you want a zero-config shared stack you can drop any project into without touching its files, prefer rootless Podman, or want the lightweight Herd-like experience on Linux or macOS.
