# Podman — Debug

[← voltar pro índice](../DEBUG.md)

## Como funciona

`lerd` roda 100% rootless em Podman 4+. Não usa Docker, não usa `sudo`, não tem daemon — tudo é executado via systemd user units (`~/.config/systemd/user/` e quadlets em `~/.config/containers/systemd/`).

```
       ~/.config/containers/systemd/
       │
       ├── lerd-php84-fpm.container      ─┐
       ├── lerd-php85-fpm.container       │  quadlets (geradores systemd)
       ├── lerd-nginx.container           │  são lidos no daemon-reload
       ├── lerd-mysql.container          ─┘  e viram unidades .service
       │
       └── lerd.network                      ─→ rede Podman compartilhada
                                                  (subnet IPv4 + IPv6 ULA)

       systemctl --user → unidades geradas    docker.io/library/<image>
                  │                                    │
                  └──── podman run/exec ───────────────┘
```

A rede `lerd` é dual-stack (IPv4 + IPv6 ULA `fd...`); containers se enxergam via DNS interno do Podman (`lerd-mysql`, `lerd-php85-fpm`, etc.) ou via `host.containers.internal`.

## Problemas comuns

### 🔴 `Failed to start lerd-php85-fpm.service: Unit not found.`

🔍 Diagnóstico:
```bash
ls ~/.config/containers/systemd/lerd-php85-fpm.container
systemctl --user daemon-reload
systemctl --user cat lerd-php85-fpm.service        # deve mostrar [X-Container] gerado
```

🟢 Conserto:
```bash
lerd php:install 8.5            # regrava quadlet + builda imagem + start
# ou pelo dashboard: System → PHP-FPM → "Instalar versão…"
```

### 🔴 Container fica em loop de restart (`activating (start) → failed`)

🔍 Diagnóstico:
```bash
journalctl --user -u lerd-php85-fpm.service --since '5 min ago' -n 100 --no-pager
podman logs lerd-php85-fpm 2>&1 | tail -50
```

Causas frequentes:
- Imagem corrupta após `lerd update`: hash mudou mas pull falhou
- Volume montado aponta para arquivo inexistente (`~/.local/share/lerd/php/.../99-xdebug.ini`)
- `php-fpm.conf` quebrado por edição manual em `~/.local/share/lerd/php/<v>/98-user.ini`

🟢 Conserto:
```bash
lerd php:rebuild 8.5 --local    # rebuild do zero, sem pull
lerd php:ini 8.5                 # edita ini com validador
```

### 🔴 `Network "lerd" not found` ao subir qualquer serviço

🔍 Diagnóstico:
```bash
podman network ls | grep lerd
cat ~/.config/containers/systemd/lerd.network 2>/dev/null
```

🟢 Conserto:
```bash
lerd install --from-update      # reaplica rede + quadlets sem refazer wizard
```

### 🔴 IPv6 falha entre containers (`connection refused` ao `lerd-mysql:3306` por exemplo)

⚠️ Acontece em distros que herdaram a rede `lerd` v4-only de uma versão antiga do lerd.

🔍 Diagnóstico:
```bash
podman network inspect lerd | jq '.[].subnets'
# Deve listar uma subnet v4 (10.X.X.X/24) E uma v6 (fd00:.../64)
```

🟢 Conserto:
```bash
lerd quit                       # para tudo
podman network rm lerd
lerd install --from-update      # recria com dual-stack
```

### 🔴 `Error: short-name "alpine" did not resolve to an alias` ao puxar imagem

🔍 Diagnóstico:
```bash
cat ~/.config/containers/registries.conf 2>/dev/null
podman pull docker.io/library/alpine:latest
```

🟢 Conserto: adicionar `unqualified-search-registries = ["docker.io"]` em `~/.config/containers/registries.conf`:
```bash
mkdir -p ~/.config/containers
printf 'unqualified-search-registries = ["docker.io"]\n' >> ~/.config/containers/registries.conf
```

### 🔴 `lerd quit` deixou containers órfãos rodando

🟢 Conserto:
```bash
podman ps -a --filter "name=lerd-" -q | xargs -r podman stop
podman ps -a --filter "name=lerd-" -q | xargs -r podman rm -f
systemctl --user reset-failed
```

## 💡 Dicas

- `systemctl --user list-units 'lerd-*' --all` mostra todas as units (incluindo failed) num único lugar.
- `podman exec -it lerd-php85-fpm sh` cai num shell zsh no container — útil pra inspecionar volumes montados.
- A flag `--security-opt=label=disable` nos quadlets é proposital: desabilita SELinux por container porque os volumes vêm de `$HOME` que tem context `unconfined_u:object_r:user_home_t:s0`.
