# Directory Layout

```
~/.config/lerd/
└── config.yaml

~/.config/containers/systemd/        # Podman Quadlet units (auto-loaded)
~/.config/systemd/user/
└── lerd-watcher.service

~/.local/share/lerd/
├── bin/                             # mkcert, fnm, static PHP binaries
├── nginx/
│   ├── nginx.conf
│   ├── conf.d/                      # one .conf per site (auto-generated)
│   ├── custom.d/                    # user overrides, preserved across updates
│   └── logs/
├── certs/
│   ├── ca/
│   └── sites/                       # per-domain .crt + .key
├── data/                            # Podman volume bind-mounts
│   ├── mysql/
│   ├── redis/
│   ├── postgres/
│   ├── meilisearch/
│   └── rustfs/
├── dnsmasq/
│   └── lerd.conf
├── vapid-private.key                # Web Push signing key (mode 0600, see features/notifications.md)
├── vapid-public.key                 # Web Push public key, served to browsers
├── push-subscriptions.json          # Browser push subscriptions + per-category prefs (mode 0600)
├── nginx-trust-token                # Per-install secret for lerd.localhost → lerd-ui proxy
└── sites.yaml
```

All directories follow the [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir-spec/latest/). Lerd never writes to system directories except during `lerd install` (DNS setup) which requires `sudo`.
