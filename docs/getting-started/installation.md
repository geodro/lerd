# Installation

## Linux

### One-line installer (recommended)

::: code-group

```bash [curl]
curl -fsSL https://raw.githubusercontent.com/geodro/lerd/main/install.sh | bash
```

```bash [wget]
wget -qO- https://raw.githubusercontent.com/geodro/lerd/main/install.sh | bash
```

```bash [From source]
git clone https://github.com/geodro/lerd
cd lerd
make build
make install            # installs to ~/.local/bin/lerd
make install-installer  # installs lerd-installer to ~/.local/bin/
```

:::

The installer will:

- Check and offer to install missing prerequisites (Podman, NetworkManager, unzip)
- Download the latest `lerd` binary for your architecture (amd64 / arm64)
- Install it to `~/.local/bin/lerd`
- Add `~/.local/bin` to your shell's `PATH` (bash, zsh, or fish)
- Automatically run `lerd install` to complete environment setup

::: info DNS setup requires sudo
`lerd install` writes to `/etc/NetworkManager/dnsmasq.d/` and `/etc/NetworkManager/conf.d/` and restarts NetworkManager. This is the only step that requires `sudo`.
:::

After install, reload your shell or open a new terminal so `PATH` takes effect.

`lerd install` will:

1. Create XDG config and data directories
2. Create the `lerd` Podman network
3. Download static binaries: Composer, fnm, mkcert
4. Install the mkcert CA into your system trust store
5. Write and start the `lerd-dns` and `lerd-nginx` Podman Quadlet containers
6. Enable the `lerd-watcher` background service (auto-discovers new projects)
7. Add `~/.local/share/lerd/bin` to your shell's `PATH`

---

### Install from a local build

If you built from source and want to skip the GitHub download:

```bash
make build
bash install.sh --local ./build/lerd
```

---

### Update

```bash
lerd update
```

Fetches the latest release from GitHub, downloads the binary for your architecture, and atomically replaces the running binary. No restart needed.

You can also re-run the installer:

::: code-group

```bash [curl]
curl -fsSL https://raw.githubusercontent.com/geodro/lerd/main/install.sh | bash -s -- --update
```

```bash [wget]
wget -qO- https://raw.githubusercontent.com/geodro/lerd/main/install.sh | bash -s -- --update
```

:::

---

### Uninstall

```bash
lerd uninstall
```

Stops all containers, disables and removes Quadlet units, removes the watcher service, removes the binary, and cleans up the `PATH` entry from your shell config. Prompts before deleting config and data directories.

To skip all prompts:

```bash
lerd uninstall --force
```

---

### Check prerequisites only

```bash
bash install.sh --check
```

---

## macOS

Install via the Homebrew tap:

```bash
brew install geodro/lerd/lerd
lerd install
```

Podman is installed automatically as a Homebrew dependency. `lerd install` sets up
Podman Machine, DNS, and nginx on first run.

**Update:**

```bash
brew upgrade lerd
lerd install
```

**Uninstall:**

```bash
lerd uninstall
brew uninstall lerd
```
