---
layout: home
title: Lerd - Local PHP development for Linux
description: Open-source local PHP development environment for Linux. Automatic .test domains, PHP 8.1–8.5, rootless Podman, built-in Web UI. Works on Ubuntu, Fedora, Arch, and Debian.

hero:
  name: Lerd
  text: Local PHP development
  tagline: Automatic .test domains, PHP 8.1–8.5, rootless Podman. Drop any project in — no config files, no Docker daemon, no sudo.
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
  - icon: 🌐
    title: Automatic .test domains
    details: Every linked project gets a .test domain instantly — no /etc/hosts edits, no DNS configuration needed.
  - icon: 🐘
    title: PHP & Node version switching
    details: Run PHP 8.1–8.5 and multiple Node.js versions simultaneously. Switch per-project with a single command or from the UI.
  - icon: 🔒
    title: One-command HTTPS
    details: lerd secure generates a trusted TLS certificate instantly via mkcert. APP_URL is updated automatically.
  - icon: 📦
    title: Rootless Podman
    details: No Docker daemon, no sudo for containers. All services run as your user — works out of the box on Arch, Ubuntu, Fedora, and Debian.
  - icon: 🔧
    title: Built-in services
    details: MySQL, Redis, PostgreSQL, Meilisearch, RustFS, and Mailpit. One command to start, shared across all your projects.
  - icon: 🖥️
    title: Web UI & system tray
    details: Browser dashboard with live logs, per-site controls, and one-click toggles. Plus a system tray applet for at-a-glance status.
---
