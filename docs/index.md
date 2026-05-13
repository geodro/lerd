---
layout: home
title: Lerd - Local PHP development for Linux
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
    title: Auto .test / .localhost
    details: Every project gets a .test domain via lerd's dnsmasq, or opt into .localhost (RFC 6761, no DNS setup). Dual-stack IPv4 + IPv6.
  - icon: 🔒
    title: One-command HTTPS
    details: "`lerd secure` issues a locally-trusted TLS cert via mkcert, rewrites the nginx vhost, and updates APP_URL in one shot."
  - icon: 🐘
    title: PHP & Node versions
    details: PHP 8.1–8.5 plus a frozen 7.4 / 8.0 legacy tier, with multiple Node versions side by side and per-project pinning from CLI, UI, or TUI.
  - icon: 🔧
    title: Services & presets
    details: Built-ins (MySQL, Postgres, Redis, Meilisearch, RustFS, Mailpit), one-click presets, or any OCI image as a custom service with depends_on.
  - icon: 🖥️
    title: Web UI, TUI & tray
    details: Browser dashboard with command palette, btop-style TUI (`lerd tui`), and a system tray applet, all wired to the same live event bus.
  - icon: 🧪
    title: Tinker tab
    details: In-browser PHP REPL per site with autocomplete, live linting, and collapsible dump trees. Works on Laravel, Symfony, and other PHP projects.
  - icon: 🛰️
    title: Live dump() / dd() viewer
    details: Every `dump()` and `dd()` call from your running app streams to the dashboard, TUI, MCP, and CLI tail, scoped per site and per worktree branch.
  - icon: 🤖
    title: AI integration (MCP)
    details: Built-in Model Context Protocol server. Claude Code, Cursor, Windsurf, and Junie scaffold projects and run migrations straight from chat.
  - icon: ⚡
    title: FrankenPHP runtime
    details: Per-site FrankenPHP as an alternative to shared PHP-FPM, with Laravel Octane and Symfony Runtime worker mode wired up out of the box.
  - icon: 📦
    title: Rootless Podman
    details: No Docker daemon, no sudo, no system pollution. Services run as your user via rootless Podman and systemd user units on Linux and macOS.
  - icon: 🧩
    title: Framework store
    details: YAML framework definitions for Laravel, Symfony, WordPress, Drupal, CakePHP, and Statamic, auto-detected and applied on `lerd link`.
  - icon: 🧱
    title: Polyglot sites
    details: Drop a `Containerfile.lerd` to run Node, Python, Ruby, or Go sites alongside your PHP ones, with the same HTTPS, DNS, and worker pipeline.
---
