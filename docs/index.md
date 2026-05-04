---
layout: home
title: Lerd - Local PHP development for Linux
description: Open-source local PHP development environment built for Linux. Nginx + PHP-FPM 8.1–8.5 on rootless Podman, with automatic .test domains, HTTPS, and a built-in Web UI. First-class on Arch, Ubuntu, Fedora, and Debian; also runs on macOS via Homebrew.

hero:
  name: Lerd
  text: Local PHP development for Linux
  tagline: Nginx + PHP-FPM 8.1–8.5 on rootless Podman. Drop any project in for automatic .test domains and HTTPS, no config files, no Docker daemon, no sudo. First-class on Arch, Ubuntu, Fedora, and Debian; macOS supported too.
  image:
    src: /assets/screenshots/app-1.png
    alt: Lerd dashboard
  actions:
    - theme: brand
      text: Get Started
      link: /getting-started/requirements
    - theme: alt
      text: View on GitHub
      link: https://github.com/geodro/lerd

features:
  - icon: 🤖
    title: AI integration (MCP)
    details: Built-in Model Context Protocol server. Claude Code, Cursor, Windsurf, and JetBrains Junie can scaffold projects, run migrations, manage services, and tail logs, all from chat.
  - icon: 📦
    title: Rootless Podman
    details: No Docker daemon, no sudo for containers, no system pollution. All services run as your user via rootless Podman and systemd user units, on Arch, Ubuntu, Fedora, and Debian (and macOS via Homebrew).
  - icon: 🌐
    title: Auto .test domains
    details: Every linked project gets a .test domain instantly, with dual-stack IPv4 and IPv6, no /etc/hosts edits, no DNS setup.
  - icon: 🐘
    title: PHP & Node versions
    details: PHP 8.1–8.5 and multiple Node versions side by side, switched per-project from the CLI, the Web UI, or the TUI.
  - icon: 🔒
    title: One-command HTTPS
    details: "`lerd secure` issues a locally-trusted TLS cert via mkcert, rewrites the nginx vhost, and updates APP_URL for you."
  - icon: ⚡
    title: FrankenPHP runtime
    details: Per-site FrankenPHP as an alternative to shared PHP-FPM, with Laravel Octane and Symfony Runtime worker mode.
  - icon: 🔧
    title: Services & presets
    details: Built-ins (MySQL, Postgres, Redis, Meilisearch, RustFS, Mailpit), one-click presets, or any OCI image as a custom service.
  - icon: 🖥️
    title: Web UI, TUI & tray
    details: Browser dashboard with command palette, btop-style TUI (`lerd tui`), and a system tray applet, all wired to the same live event bus.
  - icon: 🧪
    title: Tinker tab
    details: In-browser PHP REPL per site with autocomplete, live linting, and collapsible dump trees. Works on Laravel, Symfony, and any composer-based PHP project.
  - icon: 🧩
    title: Framework store
    details: YAML framework definitions for Laravel, Symfony, WordPress, Drupal, CakePHP, and Statamic, auto-detected on `lerd link`.
  - icon: 🧱
    title: Polyglot sites
    details: Drop a `Containerfile.lerd` to run Node, Python, Ruby, or Go alongside your PHP sites, with full HTTPS and DNS.
---
