# System Tray

`lerd tray` launches a system tray applet that gives you at-a-glance status and one-click control without opening a browser.

```bash
lerd tray              # launch (detaches from terminal automatically)
lerd tray --mono=false # use the red colour icon instead of monochrome white
```

The tray detaches from the terminal immediately, your shell prompt returns straight away.

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
  ✔ 8.5              ← current default (click to switch)
  8.4
─────────────────
Autostart at login: ✔ On   ← click to toggle (enables/disables every lerd unit)
Expose to LAN: Off         ← click to toggle (Linux only)
Debug bridge: Off           ← click to toggle `lerd dump on/off`
Notifications: ✔ On        ← click to toggle `lerd notify on/off`
⬆ Update to v0.8.3         ← shown when an update is cached; click to open terminal
Stop Lerd & Quit     ← runs lerd stop then exits the tray
```

The menu refreshes every 5 seconds. Clicking a service toggles it on/off. Clicking a PHP version sets it as the global default. "Stop Lerd & Quit" stops the entire environment before closing.

The **Debug bridge** item shells out to `lerd dump on` / `lerd dump off` — see [Dumps](dumps.md). The **Notifications** item shells out to `lerd notify on` / `lerd notify off` — see [Notifications](notifications.md). Both are global toggles, persisted to `config.yaml`.

The **Services** section shows only core services (MySQL, Redis, PostgreSQL, etc.). Per-site workers (queue, schedule, Stripe, Reverb) are managed from the web UI or via their respective CLI commands and are not listed in the tray.

The **update item** shows "Check for update..." when no update information is cached, and "⬆ Update to vX.Y.Z" once the background checker finds a newer release. Clicking it opens a terminal to run `lerd update`.

---

## Icon appearance

In the default colour mode the icon doubles as a status light: a red **L** when lerd is stopped and a white **L** when it is running. The white running icon would disappear on a light panel, so the tray reads your desktop's light/dark preference and switches the running icon to a dark **L** whenever the panel is light. It reacts live: toggle your system theme and the icon recolours without restarting the tray.

The preference comes from the cross-desktop XDG desktop portal (`org.freedesktop.appearance` `color-scheme`), so it works the same on GNOME, KDE Plasma, and anything else that implements the portal. On a setup without a portal the tray keeps the white running icon, which is the right default for the common dark panel.

The red stopped icon is left as-is since it reads on both light and dark panels. If you would rather the OS own the colouring entirely, use `lerd tray --mono`, which registers a monochrome template icon that GNOME and KDE recolour to match the panel themselves (this mode does not change with lerd state).

---

## Autostart

The tray follows the global `lerd autostart` toggle: when autostart is on (the default), `lerd install` writes and enables `lerd-tray.service` so the tray comes up on every graphical login. Run `lerd autostart disable` to turn off autostart for the entire environment, including the tray.

The tray is also started automatically by `lerd start` if it isn't already running.

The unit is wired to `graphical-session.target`, which is reached automatically by GNOME, KDE Plasma, and any Wayland compositor launched through `uwsm` (including Omarchy's Hyprland setup). On bare Hyprland / Sway / i3 launched without `uwsm`, `graphical-session.target` is never started, so the tray will not autostart. Either run the compositor under `uwsm` or replace `WantedBy=graphical-session.target` with `WantedBy=default.target` in `~/.config/systemd/user/lerd-tray.service`.

---

## Desktop environment compatibility

The tray uses the **StatusNotifierItem (SNI) / AppIndicator** protocol (DBus-based).

| Environment | Status |
|---|---|
| KDE Plasma | Works out of the box |
| GNOME | Requires the [AppIndicator and KStatusNotifierItem Support](https://extensions.gnome.org/extension/615/appindicator-support/) extension |
| Sway / Hyprland with waybar | Works with `"tray"` module in waybar config |
| i3 with i3bar | Requires [snixembed](https://git.sr.ht/~yerlan/snixembed) to bridge SNI to XEmbed |
| XFCE / LXQt | Works out of the box |

---

## Build requirements

The tray uses CGO and requires `libayatana-appindicator` at build time:

::: code-group

```bash [Arch / CachyOS / omarchy]
sudo pacman -S libayatana-appindicator
```

```bash [Debian / Ubuntu]
sudo apt install libayatana-appindicator3-dev
```

```bash [Fedora]
sudo dnf install libayatana-appindicator-gtk3
```

:::

For headless / CI builds without the tray:

```bash
make build-nogui   # produces ./build/lerd-nogui (lerd tray returns an error)
```
