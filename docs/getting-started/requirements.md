# Requirements

## Linux

- **Distribution**: Arch, Debian/Ubuntu, Fedora-based, or omarchy
- **[Podman](https://podman.io/)** 4.5 or newer, rootless, with systemd user session active
- **[crun](https://github.com/containers/crun)**: recommended OCI runtime for rootless Podman
- **DNS resolver**: [NetworkManager](https://networkmanager.dev/) or [systemd-resolved](https://www.freedesktop.org/software/systemd/man/systemd-resolved.service.html) (at least one is required for `.test` DNS)
- **`systemctl --user` functional**: run `loginctl enable-linger $USER` if needed

### Podman 4.5 minimum

::: warning
Lerd creates the `lerd` podman network with `podman network create --dns`, a flag added in podman 4.5 (April 2023). Older releases fail install with `Error: unknown flag: --dns`. Distribution defaults that ship podman older than 4.5:

| Distro                 | Default podman | Workaround                                                                       |
|------------------------|---------------:|----------------------------------------------------------------------------------|
| Ubuntu 22.04           | 3.4.4          | Install a newer podman from the [Kubic libcontainers OBS repo](https://podman.io/docs/installation#ubuntu-2204-2104-2010-2004) |
| Zorin 17               | 3.4.4          | Same Kubic instructions as Ubuntu 22.04 (Zorin 17 is jammy-based)                |
| Debian 12 (bookworm)   | 4.3.1          | `sudo apt install -t bookworm-backports podman` (ships 4.9+)                     |
| Debian 11 (bullseye)   | 3.0.1          | Upgrade to Debian 12 + enable bookworm-backports                                 |

Fedora 38+, Ubuntu 24.04+, openSUSE Tumbleweed, Arch and CachyOS all ship podman 4.5 or newer out of the box.
:::

::: warning Linger must be enabled
If `systemctl --user` units do not survive logout, run:
```bash
loginctl enable-linger $USER
```
This is required for Podman Quadlet containers to start automatically and persist across sessions.
:::

::: tip crun is the recommended OCI runtime
Most distributions ship `crun` as the default rootless Podman runtime. On Arch-based systems, `runc` is the default and `crun` must be installed separately. While both runtimes work, `crun` is lighter and purpose-built for rootless containers. `lerd doctor` will warn if `crun` is not installed.

```bash
# Arch / omarchy
sudo pacman -S crun

# Debian / Ubuntu
sudo apt install crun

# Fedora
sudo dnf install crun
```
:::

- **`unzip`**: used during install to extract fnm
- **`certutil` / `nss-tools`**: for mkcert to install the CA into Chrome/Firefox
    - Arch: `nss`
    - Debian/Ubuntu: `libnss3-tools`
    - Fedora: `nss-tools`

::: tip Go is only needed to build from source
The released binary is fully static with no runtime dependencies. You do not need Go installed to use Lerd.
:::

## macOS

- **macOS 13 Ventura or later**: Apple Silicon (arm64) or Intel (amd64)
- **[Homebrew](https://brew.sh/)**: used to install lerd and its Podman dependency
- **[Podman](https://podman.io/)**: installed automatically as a Homebrew dependency of `lerd`
- **Podman Machine**: `lerd install` boots and configures it on first run
- **Xcode Command Line Tools**: required by Homebrew (`xcode-select --install` if missing)

DNS, the local CA (mkcert), and nginx are all set up by `lerd install`. No system-level resolver configuration is needed; macOS picks up `.test` lookups from `/etc/resolver/test` which lerd writes for you.

### Podman Machine memory

On first start `lerd` sizes the Podman Machine VM based on your host RAM so 8 GB MacBooks aren't squeezed while larger machines get headroom for heavier workloads.

| Host RAM | Podman Machine memory |
|----------|-----------------------|
| ≤ 8 GB   | 3 GB                  |
| 9-31 GB  | 4 GB                  |
| ≥ 32 GB  | 6 GB                  |

The memory value is a ceiling, not a reservation: the VM only uses what your containers actually request. If sites slow down under load on an 8 GB host, bump the VM manually:

```bash
podman machine stop
podman machine set --memory 4096
podman machine start
```
