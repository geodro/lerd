# Lerd

> Open-source Herd-like local PHP development environment for Linux.
> Podman-native, rootless, with a built-in Web UI.

[![CI](https://github.com/geodro/lerd/actions/workflows/ci.yml/badge.svg)](https://github.com/geodro/lerd/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/geodro/lerd)](https://github.com/geodro/lerd/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-Linux-lightgrey)]()
[![Docs](https://img.shields.io/badge/docs-geodro.github.io%2Flerd-blue)](https://geodro.github.io/lerd/)

![Lerd Web UI](screenshots/app-1.png)

Lerd runs Nginx, PHP-FPM, and your services as rootless Podman containers,
designed for PHP developers on Linux (Ubuntu, Fedora, Arch, Debian).
No Docker. No sudo. No system pollution. Just `lerd link` and your project
is live at `project.test` with HTTPS.

## Built for Linux PHP developers

If you're a PHP developer on Linux and want frictionless local development — automatic `.test` domains, per-project PHP versions, one-click HTTPS, zero Docker — Lerd is built for you. Works with Laravel, Symfony, WordPress, Drupal, CakePHP, Statamic, and any custom PHP framework.

## Features

- 🌐 **Automatic `.test` domains** with one-command TLS
- 🐘 **Per-project PHP version** (8.1–8.5), switch with one click
- 📦 **Node.js isolation** per project (Node 22, 24)
- 🖥️ **Built-in Web UI** - 3-pane dashboard to manage sites, services, and logs from a browser
- 🗄️ **One-click services**: MySQL, PostgreSQL, Redis, Meilisearch, RustFS, Mailpit, Stripe Mock, Reverb and more
- 📋 **Live logs** for PHP-FPM, Queue, Schedule, Reverb, per site
- 🔒 **Rootless & daemonless** - Podman-native, no Docker required
- 🤖 **MCP server** - let AI assistants (Claude Code, Windsurf, Junie) manage your environment directly
- 🧩 **Framework store** - community definitions for Laravel, Symfony, WordPress, Drupal, CakePHP, Statamic with versioned auto-detection
- ⚡ **Framework-agnostic** workers, env setup, and nginx proxy — driven by YAML definitions, not hardcoded

## AI Integration (MCP)

Lerd ships a built-in [Model Context Protocol](https://modelcontextprotocol.io/) server. Connect it to Claude Code, Windsurf, JetBrains Junie, or any MCP-compatible AI assistant and manage your dev environment without leaving the chat.

```bash
lerd mcp:enable-global   # register once, works in every project
```

Then just ask:

```
You: set up the project I just cloned
AI:  → site_link()
     → env_setup()    # detects MySQL + Redis, starts them, creates DB, generates APP_KEY
     → composer install
     → artisan migrate --seed
     ✓  myapp → https://myapp.test ready
```

60+ tools available: run migrations, manage services, toggle workers, tail logs, enable Xdebug, manage databases, manage PHP extensions, park directories, and more, all from your AI assistant.

📖 [MCP documentation](https://geodro.github.io/lerd/features/mcp/)

## Why Lerd?

|                    | Lerd | DDEV | Lando | Laravel Herd |
|--------------------|------|------|-------|--------------|
| Podman-native      | ✅   | ❌   | ❌    | ❌           |
| Rootless           | ✅   | ❌   | ❌    | ✅           |
| Web UI             | ✅   | ❌   | ❌    | ✅           |
| Linux              | ✅   | ✅   | ✅    | ❌           |
| macOS              | 🔜   | ✅   | ✅    | ✅           |
| MCP server         | ✅   | ❌   | ❌    | ❌           |
| Free & open source | ✅   | ✅   | ✅    | ❌           |

## Install

### Linux

```bash
curl -fsSL https://raw.githubusercontent.com/geodro/lerd/main/install.sh | bash
```

Update later with:

```bash
lerd update
```

### macOS

Install via Homebrew:

```bash
brew install geodro/lerd/lerd
lerd install
```

Update later with:

```bash
brew upgrade lerd
lerd install
```

> [!NOTE]
> See the [installation docs](https://geodro.github.io/lerd/getting-started/installation/) for details.

## Quick Start

```bash
cd my-laravel-project
lerd link
# → https://my-laravel-project.test
```

`lerd install` already starts everything for you on first run, so you can `lerd link` immediately. Day-to-day:

```bash
lerd start          # boot DNS, nginx, PHP-FPM, services, workers, UI
lerd stop           # stop containers and workers (UI and watcher stay up)
lerd quit           # full shutdown including UI, watcher, and tray
lerd autostart enable   # boot lerd on every login
lerd status         # health snapshot
```

See [Start, Stop & Autostart](https://geodro.github.io/lerd/usage/lifecycle/) for the full lifecycle reference.

## Framework Store

Install community framework definitions from [geodro/lerd-frameworks](https://github.com/geodro/lerd-frameworks):

```bash
lerd framework search                   # list all available
lerd framework install symfony          # auto-detects version from composer.lock
lerd framework install drupal@11        # explicit version
lerd framework list --check             # compare local vs store
```

Frameworks auto-detect when you `lerd link` a project. Workers, env setup, nginx proxy, and setup commands are all driven by the framework definition — no hardcoded behavior.

## Documentation

📖 **[geodro.github.io/lerd](https://geodro.github.io/lerd/)**

- [Requirements](https://geodro.github.io/lerd/getting-started/requirements/)
- [Installation](https://geodro.github.io/lerd/getting-started/installation/)
- [Quick Start](https://geodro.github.io/lerd/getting-started/quick-start/)
- [Start, Stop & Autostart](https://geodro.github.io/lerd/usage/lifecycle/)
- [Frameworks](https://geodro.github.io/lerd/usage/frameworks/)
- [Services](https://geodro.github.io/lerd/usage/services/)
- [Command Reference](https://geodro.github.io/lerd/reference/commands/)

## License

MIT
