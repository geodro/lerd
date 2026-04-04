# System Tray

`lerd tray` launches a system tray applet that gives you at-a-glance status and one-click control without opening a browser.

```bash
lerd tray              # launch (detaches from terminal automatically)
lerd tray --mono=false # use the red colour icon instead of monochrome white
```

The tray detaches from the terminal immediately — your shell prompt returns straight away.

---

## Menu layout

```
🟢 Running          ← overall status (disabled, informational)
  🟢 nginx
  🟢 dns
─────────────────
Open Dashboard       ← opens http://127.0.0.1:7073
Stop Lerd            ← toggles between Start / Stop Lerd
─────────────────
── Services ──
  🟢 mysql           ← click to stop
  🔴 redis           ← click to start
─────────────────
── PHP ──
  ✔ 8.4              ← current default (click to switch)
  8.3
─────────────────
Autostart at login: ✔ On   ← click to toggle
⬆ Update to v0.8.3         ← shown when an update is cached; click to open terminal
Stop Lerd & Quit     ← runs lerd stop then exits the tray
```

The menu refreshes every 5 seconds. Clicking a service toggles it on/off. Clicking a PHP version sets it as the global default. "Stop Lerd & Quit" stops the entire environment before closing.

The **Services** section shows only core services (MySQL, Redis, PostgreSQL, etc.). Per-site workers (queue, schedule, Stripe, Reverb) are managed from the web UI or via their respective CLI commands and are not listed in the tray.

The **update item** shows "Check for update..." when no update information is cached, and "⬆ Update to vX.Y.Z" once the background checker finds a newer release. Clicking it opens a terminal to run `lerd update`.

---

## Autostart

To have the tray start automatically when you log in:

```bash
lerd autostart tray enable
lerd autostart tray disable
```

The tray is also started automatically by `lerd start` if it isn't already running.

---

## Desktop environment compatibility

The tray uses the **StatusNotifierItem (SNI) / AppIndicator** protocol (DBus-based).

| Environment | Status |
|---|---|
| KDE Plasma | Works out of the box |
| GNOME | Requires the [AppIndicator and KStatusNotifierItem Support](https://extensions.gnome.org/extension/615/appindicator-support/) extension |
| Sway / Hyprland with waybar | Works with `"tray"` module in waybar config |
| i3 with i3bar | Requires [snixembed](https://git.sr.ht/~yerlan/snixembed) to bridge SNI → XEmbed |
| XFCE / LXQt | Works out of the box |

---

## Build requirements

The tray uses CGO and requires `libappindicator3` at build time:

::: code-group

```bash [Arch]
sudo pacman -S libappindicator-gtk3
```

```bash [Debian / Ubuntu]
sudo apt install libappindicator3-dev
```

```bash [Fedora]
sudo dnf install libappindicator-gtk3-devel
```

:::

For headless / CI builds without AppIndicator:

```bash
make build-nogui   # produces ./build/lerd-nogui — lerd tray returns an error
```
