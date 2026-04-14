# Lerd vs Other Tools

## Lerd vs Laravel Sail

[Laravel Sail](https://laravel.com/docs/sail) is the official per-project Docker Compose solution. Lerd is a shared infrastructure approach, closer to what [Laravel Herd](https://herd.laravel.com/) does on macOS. Both are valid — they solve slightly different problems.

|  | Lerd | Laravel Sail |
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
| Team parity (all OS) | Linux, macOS | macOS, Windows, Linux |

**Choose Sail when:** your team uses it, you need per-project service versions, or you want infrastructure defined in the repo.

**Choose Lerd when:** you work across many projects at once and don't want a separate stack per repo, you can't modify project files, you want instant `.test` routing, or you want the Herd experience on Linux as well as macOS.

### Migrating from Sail to Lerd

`lerd import sail` imports a Sail project's database and S3/MinIO files into lerd in one command — no manual dump/restore needed. It starts Sail temporarily with remapped ports (so there are no conflicts with lerd's running services), dumps the database, mirrors storage files, then tears Sail back down.

```bash
cd ~/Projects/myapp
lerd sail import
```

See [Importing from Laravel Sail](/usage/import-sail) for details.

---

## Lerd vs ddev

[ddev](https://ddev.com/) is a popular open-source local development tool that spins up per-project Docker containers with a shared Traefik router. It supports many frameworks (Laravel, WordPress, Drupal, etc.) and runs on macOS, Windows, and Linux. Lerd is narrower in scope — Laravel-focused, Podman-native, shared infrastructure — closer to the Herd model.

|  | Lerd | ddev |
|---|---|---|
| Container runtime | Rootless Podman | Docker (or Orbstack / Colima) |
| Architecture | Shared Nginx + PHP-FPM across all projects | Per-project containers + shared Traefik router |
| Services (MySQL, Redis…) | One shared instance | Per-project (isolated by default) |
| Domains | `.test` — automatic, zero config | `.ddev.site` or custom — automatic via Traefik |
| HTTPS | `lerd secure` → trusted cert instantly | Built-in via mkcert |
| RAM with 5 projects running | ~200 MB | ~500 MB–1 GB (5× app containers + router) |
| Requires changes to project files | No | Yes — needs `.ddev/config.yaml` committed |
| Works on legacy / client repos | Yes — just `lerd link` | Only if you can add ddev config |
| Framework support | Laravel built-in; any PHP framework via YAML definitions | Laravel, WordPress, Drupal, and many more |
| Defined in code (infra-as-code) | No | Yes |
| Team parity (all OS) | Linux, macOS | macOS, Windows, Linux |

**Choose ddev when:** your team is cross-platform, you work with multiple frameworks (not just Laravel), you want per-project service isolation, or your workflow already depends on Docker.

**Choose Lerd when:** you want a zero-config shared stack you can drop any project into without touching its files, prefer rootless Podman, or want the lightweight Herd-like experience on Linux or macOS.
