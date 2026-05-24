# Updates do fork — Debug

[← voltar pro índice](../DEBUG.md)

## Como funciona

O fork tem seu próprio canal de release independente do upstream `geodro/lerd`:

- **Repo do release**: `https://github.com/gabriel-sousa99/lerd/releases`
- **Esquema de versão**: `v1.21.2-oracle.N` onde `N` cresce a cada release do fork
- **Comparador**: `1.21.2-oracle.2 > 1.21.2-oracle.1 > 1.21.2` (upstream)
  - `-oracle.N` é tratado como *fork build*, NÃO prerelease
  - Stable users veem update notificações pra novos `oracle.N`
- **Auto-update**: `lerd update` consulta o fork (`/releases/latest` → tag), baixa o `tar.gz`, substitui o binário atomicamente, roda `lerd install --from-update` e, se a Containerfile mudou, `lerd php:rebuild`

```
   lerd update
        │
        ▼
   GET /releases/latest  (gabriel-sousa99/lerd)
        │ redirect
        ▼
   /releases/tag/v1.21.2-oracle.N
        │
        ▼
   download lerd_1.21.2-oracle.N_linux_amd64.tar.gz
        │
        ▼
   extract → atomic mv → install --from-update → (talvez) php:rebuild
```

## Problemas comuns

### 🔴 `lerd update` diz "Already on latest" mas eu sei que tem versão nova

🔍 Diagnóstico:
```bash
cat ~/.cache/lerd/update_check.json
# Mostra última verificação. Tem TTL de 24h.
lerd about | head -3
```

🟢 Conserto: força refresh:
```bash
rm ~/.cache/lerd/update_check.json
lerd update
```

### 🔴 `lerd update` falhou no download (`download failed: ... HTTP 404`)

🔍 Diagnóstico:
```bash
curl -sI https://github.com/gabriel-sousa99/lerd/releases/latest
# Espera-se HTTP/2 302 com Location: .../tag/v1.21.2-oracle.N
```

🟢 Conserto: provavelmente release ainda não foi publicado (só tag existe). Aguarde alguns minutos ou:
```bash
# Instalar manualmente da release anterior:
gh release download v1.21.2-oracle.<anterior> --repo gabriel-sousa99/lerd -D /tmp/
tar -tzf /tmp/lerd_*.tar.gz
install -Dm755 lerd ~/.local/bin/lerd
```

### 🔴 Após `lerd update`, binário está corrupto / segfault

🟢 Conserto: rollback automático:
```bash
lerd update --rollback                              # volta pra versão anterior (backup automático)
# Backup fica em ~/.local/share/lerd/binary-backups/
ls -la ~/.local/share/lerd/binary-backups/
```

### 🔴 `lerd update` rodou mas `lerd about` mostra a versão antiga

🔍 Diagnóstico:
```bash
which lerd                                          # caminho real
realpath $(which lerd)                              # se é symlink
ls -la ~/.local/bin/lerd
```

🟢 Conserto: tem mais de uma cópia do binário. PATH provavelmente está pegando outra:
```bash
echo $PATH | tr ':' '\n'
# Mover ~/.local/bin pra antes
```

### 🔴 Após update, FPM containers não voltam

⚠️ O `lerd update` roda `lerd install --from-update` que faz daemon-reload + start dos units. Se o hash do Containerfile mudou, ele tenta `lerd php:rebuild` em seguida.

🔍 Diagnóstico:
```bash
journalctl --user -u 'lerd-php*-fpm.service' -n 30 --no-pager
podman images | grep lerd-php
```

🟢 Conserto:
```bash
lerd php:rebuild --local                            # força build do zero, sem pull
```

### 🔴 Quero ficar numa versão specific e não receber notif de update

🟢 Conserto:
```bash
# Notificação de update aparece via lerd doctor e dashboard. Pra silenciar:
lerd notify off                                     # desabilita notificações em geral
```

### 🔴 Buildando do código do fork em outra máquina

```bash
git clone https://github.com/gabriel-sousa99/lerd.git
cd lerd
git checkout v1.21.2-oracle.N                       # ou main pra latest dev

# Instalar Go 1.25+:
curl -fsSL https://go.dev/dl/go1.25.3.linux-amd64.tar.gz | tar -C ~/.local -xzf -
export PATH=$HOME/.local/go/bin:$PATH

# Build UI:
cd internal/ui/web && npm ci && npm run build && cd ../../..

# Build binário:
CGO_ENABLED=0 go build -tags nogui \
  -ldflags="-s -w -X github.com/geodro/lerd/internal/version.Version=$(git describe --tags)" \
  -o build/lerd ./cmd/lerd

install -Dm755 build/lerd ~/.local/bin/lerd
lerd install
```

### 🔴 `lerd-installer --update` apontou pro upstream e perdi as features Oracle

⚠️ Aconteceu se você mantinha o `install.sh` antigo (do upstream) no PATH. O `install.sh` deste fork tem `REPO="gabriel-sousa99/lerd"`.

🟢 Conserto:
```bash
curl -fsSL https://raw.githubusercontent.com/gabriel-sousa99/lerd/main/install.sh -o ~/.local/bin/lerd-installer
chmod +x ~/.local/bin/lerd-installer
lerd-installer --update
```

## 💡 Dicas

- `lerd update --beta` puxa pre-releases (`-oracle.N-beta.M` se algum dia for usado).
- Sempre que receber update, o lerd recria a `~/.config/containers/systemd/lerd.network` se o template mudou. Sites continuam linkados, vhosts mantidos.
- O comparador é case-sensitive: `oracle.1` é diferente de `Oracle.1`. Use sempre lowercase nas tags.
- O `Date` em `lerd about` mostra o timestamp UTC do build, não da release.
