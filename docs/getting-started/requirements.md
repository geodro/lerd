# Requirements

- **Linux** — Arch, Debian/Ubuntu, or Fedora-based
- **[Podman](https://podman.io/)** — rootless, with systemd user session active
- **[NetworkManager](https://networkmanager.dev/)** — for `.test` DNS
- **`systemctl --user` functional** — run `loginctl enable-linger $USER` if needed

::: warning Linger must be enabled
If `systemctl --user` units do not survive logout, run:
```bash
loginctl enable-linger $USER
```
This is required for Podman Quadlet containers to start automatically and persist across sessions.
:::

- **`unzip`** — used during install to extract fnm
- **`certutil` / `nss-tools`** — for mkcert to install the CA into Chrome/Firefox
    - Arch: `nss`
    - Debian/Ubuntu: `libnss3-tools`
    - Fedora: `nss-tools`

::: tip Go is only needed to build from source
The released binary is fully static with no runtime dependencies. You do not need Go installed to use Lerd.
:::
