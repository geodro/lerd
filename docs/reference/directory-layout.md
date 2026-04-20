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
└── sites.yaml
```

All directories follow the [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir-spec/latest/). Lerd never writes to system directories except during `lerd install` (DNS setup) which requires `sudo`.
