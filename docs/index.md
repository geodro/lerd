---
layout: home
title: Lerd - Local PHP development for Linux
titleTemplate: false
description: Open-source local PHP development environment built for Linux. Nginx + PHP on rootless Podman, with automatic .test domains, HTTPS, and a built-in Web UI. First-class on Arch, Ubuntu, Fedora, and Debian; also runs on macOS via Homebrew.

hero:
  name: Lerd
  text: Local PHP development for Linux
  tagline: Nginx + PHP on rootless Podman. Drop any project in for automatic .test domains and HTTPS, no config files, no Docker daemon, no sudo. First-class on Arch, Ubuntu, Fedora, and Debian; macOS supported too.
  image:
    src: /assets/screenshots/tour.gif
    alt: Lerd dashboard
  actions:
    - theme: brand
      text: Get Started
      link: /getting-started/requirements
    - theme: alt
      text: View on GitHub
      link: https://github.com/geodro/lerd

features:
  - icon: 🌐
    title: Auto .test domains & HTTPS
    details: Every project gets a .test domain via lerd's dnsmasq (or opt into .localhost, no DNS setup), and `lerd secure` issues a locally-trusted TLS cert with mkcert in one shot. Dual-stack IPv4 + IPv6.
  - icon: 🐘
    title: PHP, Node & bun, per project
    details: PHP 8.1–8.5 plus a frozen 7.4 / 8.0 legacy tier, multiple Node versions side by side, and bun as a first-class JS runtime, all pinned per project from the CLI, UI, or TUI.
  - icon: ⚡
    title: FrankenPHP & Octane
    details: Per-site FrankenPHP as an alternative to shared PHP-FPM, with full extension parity, Laravel Octane and Symfony worker mode, and auto-reload on file changes.
  - icon: 🔧
    title: Services & presets
    details: Built-ins (MySQL, Postgres, Redis, Meilisearch, RustFS, Mailpit), one-click presets, or any OCI image as a custom service, each editable and updatable in place.
  - icon: 🖥️
    title: Web UI, TUI & tray
    details: A browser dashboard with command palette, Laravel Doctor and a live Resources view, a btop-style TUI (`lerd tui`), and a system tray applet, all on one event bus.
  - icon: 🛰️
    title: Debug window & profiler
    details: Live `dump()` / `dd()` capture, SQL queries with N+1 detection, mail, jobs and outgoing HTTP across Laravel and Symfony, plus a one-toggle SPX flame-graph profiler.
  - icon: 🤖
    title: AI integration (MCP)
    details: A built-in Model Context Protocol server with eleven grouped tools. Claude Code, Cursor, Codex, Gemini, Copilot, Antigravity, Junie, and Windsurf scaffold and run migrations from chat.
  - icon: 📦
    title: Rootless & polyglot
    details: No Docker daemon, no sudo. Services run as your user via rootless Podman, and a `Containerfile.lerd` or host-proxy site runs Node, Python, Go or Ruby apps alongside your PHP ones.
---
