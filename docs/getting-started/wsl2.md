# Windows (WSL2)

Lerd runs on Windows through WSL2. There is no native Windows build, the architecture leans on systemd user services and rootless Podman, both of which only exist on Linux. WSL2 with systemd enabled gives you a real Linux user session where the standard Linux build runs unchanged, install script, Quadlets, watcher and all.

This page is the minimum configuration to get a working setup, with the WSL2 specific gotchas called out so you do not lose an afternoon to them.

::: warning Beta
Windows support via WSL2 is **beta**. The standard Linux build runs unchanged inside a systemd WSL2 session and is fine for daily development, but this path gets less testing than native Linux or macOS and a couple of steps still need manual Windows-side setup (mirrored networking and trusting the mkcert root CA, both below). Please report anything that misbehaves on the [issue tracker](https://github.com/geodro/lerd/issues).
:::

::: warning Supported distros
Ubuntu 22.04 or newer, or Debian 12 or newer, running on WSL 0.67.6 or newer (the release that brought official systemd support). Older WSL versions need workarounds like `genie` or `subsystemctl`, which are not tested here.
:::

## Automated setup

Once lerd is installed (step 4 below), most of this page is one command:

```bash
lerd wsl:setup
```

It's idempotent and applies the WSL-specific tweaks for you: enabling systemd in `/etc/wsl.conf`, setting the Podman `events_logger`, turning on mirrored networking in your Windows `.wslconfig`, importing the mkcert root CA into the Windows trust store, and masking the tray. It prints anything it couldn't do (for example when Windows interop isn't reachable) so you can apply that step by hand. The one thing it can't do for you is the reboot, afterwards run `wsl --shutdown` from a Windows prompt and reopen the distro.

`lerd doctor` adds a `[WSL2]` section that re-checks these on demand.

The sections below document each step manually, both as the reference for what `lerd wsl:setup` does and as the fallback if you'd rather do it by hand.

## 1. Enable systemd inside WSL2

The whole architecture, Quadlet containers, watcher, UI, FPM, runs as systemd user units. Without systemd as PID 1, nothing starts.

Inside the WSL distro, create or edit `/etc/wsl.conf`:

```ini
[boot]
systemd=true

[user]
default=your-user
```

Then from PowerShell on Windows:

```powershell
wsl --shutdown
```

Reopen the WSL terminal and confirm both checks pass:

```bash
ps -p 1 -o comm=
# expected: systemd

systemctl --user status
# should respond without error
```

## 2. Mirrored networking (recommended)

Without mirrored networking, `http://yoursite.test` and `http://yoursite.localhost` only respond inside the WSL distro, the Chrome or Edge install on the Windows side cannot reach them. With mirrored networking the WSL2 network interface mirrors the Windows one and loopback works from both sides.

On Windows, create or edit `%USERPROFILE%\.wslconfig`:

```ini
[wsl2]
networkingMode=mirrored
dnsTunneling=true
firewall=true
autoProxy=true
```

Then `wsl --shutdown` again.

::: info Requires WSL 2.0.0+ on Windows 11 22H2+
Without mirrored networking you can still reach the dashboard from a browser installed inside WSL (Firefox or Chromium from apt) or by hitting the WSL VM IP directly (`ip addr show eth0`), but day to day this is the setting that makes Windows browsers behave like the WSL distro is localhost.
:::

## 3. Install Podman

Podman in default WSL2 Ubuntu and Debian repos works, but a few defaults clash with how lerd drives the Quadlets. The cleanest path is to follow [this Podman on WSL2 setup gist](https://gist.github.com/GiovanniGrieco/94ab72099fa35bc307fda0e36b88f1bd), then apply the lerd specific tweak below.

```bash
sudo apt update
sudo apt install -y \
  podman uidmap fuse-overlayfs slirp4netns \
  curl git unzip libnss3-tools
```

`uidmap` and `slirp4netns` are what give you rootless containers. `fuse-overlayfs` is the fallback when the WSL kernel's overlayfs misbehaves. `libnss3-tools` provides the `certutil` mkcert needs.

::: danger Set `events_logger = "journald"` for Podman
Grieco's gist sets `events_logger = "file"` in `~/.config/containers/containers.conf`. Podman defaults to the `journald` log driver on systemd hosts, and refuses `--follow` when the log driver is journald but the events backend is file. Lerd's dashboard log views and `lerd logs` both call `podman logs --follow` under the hood, so every log pane ends up showing `Error: using --follow with the journald --log-driver but without the journald --events-backend (file) is not supported`.

Set `events_logger = "journald"` instead:

```ini
# ~/.config/containers/containers.conf
[engine]
events_logger = "journald"
cgroup_manager = "cgroupfs"
```

Keep `cgroup_manager = "cgroupfs"` from the gist, that part is correct for WSL2.
:::

::: warning Do not install Docker alongside Podman
Lerd is built exclusively for rootless Podman with Quadlet. Installing `docker.io` or `docker-ce` on the same WSL distro causes networking and cgroup conflicts.
:::

## 4. Run the lerd installer

```bash
curl -fsSL https://raw.githubusercontent.com/geodro/lerd/main/install.sh | bash
```

When the installer asks **"Let lerd manage DNS for local sites?"**, both modes are viable on WSL2:

- **Yes (`.test` domains, dnsmasq, HTTPS)**: confirmed working on WSL2 Ubuntu by a community user. Picks up `systemd-resolved` or NetworkManager if you have one running, and falls back cleanly when neither is the active resolver.
- **No (`.localhost` domains, no DNS daemon)**: lighter path, no resolver wiring at all, `.localhost` resolves to loopback by RFC 6761. Good if you hit DNS issues with the `.test` mode.

If the installer fails to enable linger automatically, run it by hand and then restart the distro:

```bash
sudo loginctl enable-linger $USER
exit
# in PowerShell:
wsl --shutdown
```

## 5. Keep projects in `$HOME`, never in `/mnt/c/...`

This is the single biggest performance lever on WSL2. Bind mounts from `/mnt/c/...` into containers route through 9P, which is roughly an order of magnitude slower than the WSL2 ext4 filesystem. `composer install` and `npm install` are where you feel it.

```bash
# Avoid
cd /mnt/c/Users/you/projects/myapp
lerd link

# Do
mkdir -p ~/projects
cd ~/projects
git clone git@github.com:org/myapp.git
cd myapp
lerd link
```

If you edit from VS Code on Windows, use the Remote-WSL extension and launch from the WSL side with `code .` from inside `~/projects/myapp`. The Windows VS Code process will attach to the WSL server, but the files stay on ext4.

## 6. HTTPS and the mkcert root CA

`lerd secure` installs the mkcert root CA into the WSL trust store, not the Windows one. So out of the box:

- A browser installed inside WSL (Firefox or Chromium from apt) trusts the cert.
- Chrome, Edge, Firefox on Windows do not.

To get a Windows browser to trust lerd certs, export the root CA out of WSL and import it on the Windows side:

```bash
cp "$(mkcert -CAROOT)/rootCA.pem" /mnt/c/Users/$USER/Desktop/lerd-rootCA.crt
```

Then on Windows, double-click `lerd-rootCA.crt`. In the Certificate Import Wizard, pick **Place all certificates in the following store**, browse to **Trusted Root Certification Authorities**, and finish the import. Restart the browser. From then on `https://*.test` is trusted from Windows.

If you don't need HTTPS, plain `http://yoursite.test` (or `.localhost`) works from a Windows browser as soon as mirrored networking is on.

## 7. The system tray service does not work on WSL2

`lerd-tray` needs a graphical tray host implementing the `StatusNotifierItem` or `AppIndicator` protocol, and WSL2 does not provide one. Nothing else is affected, the CLI and the dashboard at `http://lerd.localhost` (or `http://127.0.0.1:7073` directly) cover everything.

Mask the unit so it stops complaining in logs:

```bash
systemctl --user mask lerd-tray.service
```

## Verifying the install

Before running `lerd link` on your first project, sanity check that all the pieces are in place:

```bash
ps -p 1 -o comm=                                   # systemd
systemctl --user is-active default.target          # active
loginctl show-user $USER --property=Linger         # Linger=yes
podman info --format '{{.Host.Security.Rootless}}' # true
podman info --format '{{.Store.GraphDriverName}}'  # overlay
systemctl --user is-active lerd-ui                 # active
getent hosts lerd.localhost                        # ::1 / 127.0.0.1
```

If all of those come back green, `cd ~/projects/myapp && lerd link` behaves exactly the same as on native Linux.

## Known WSL2 quirks

::: details Dashboard shows "system resolver isn't routing your domains to it"
The dashboard runs a check that compares the host's resolver against `lerd-dns`. On WSL2 the resolver is `wsl.localhost` (managed by Windows), and lerd's per-container DNS still works correctly even when the host resolver isn't pointed at `lerd-dns`. Sites resolve, the warning is benign on WSL2 today.
:::

::: details Composer or npm install is painfully slow
The project is somewhere under `/mnt/c/...`. Move it into `~/projects/` and re-link.
:::

::: details `lerd doctor` complains about NetworkManager dispatcher hooks
You picked the `.test` mode and your distro has neither `systemd-resolved` nor NetworkManager managing DNS. Either install one, or reinstall and pick the `.localhost` mode.
:::

::: details `podman build` fails on overlay
`sudo apt install fuse-overlayfs && podman system reset` then run install again.
:::

::: details `cannot allocate memory` on large builds
Raise the WSL VM memory ceiling in `%USERPROFILE%\.wslconfig`:

```ini
[wsl2]
memory=8GB
```

Then `wsl --shutdown` and reopen.
:::

::: details Port 80 or 443 already in use
Usually a leftover nginx from Valet for Linux or a stale Docker Desktop service. Stop and disable the offending unit, then `lerd stop && lerd start`.
:::

## What about a native Windows build?

A native Windows port is not on the roadmap. The runtime depends on rootless Podman with Quadlet plus systemd user units, neither of which has a Windows equivalent. Maintaining a third fully separate runtime path next to Linux and the macOS launchd port is not realistic without sustained Windows-side help. WSL2 is the supported way to run lerd on Windows.
