# Services (MySQL, Postgres, Redis, …) — Debug

[← voltar pro índice](../DEBUG.md)

## Como funciona

Cada service preset é um YAML em `internal/config/presets/<service>.yaml` embarcado no binário. Quando você roda `lerd service start mysql`, lerd:

1. Lê o preset (image, ports, env_vars, data_dir, etc.)
2. Gera um quadlet em `~/.config/containers/systemd/lerd-mysql.container`
3. `systemctl --user daemon-reload && start lerd-mysql.service`
4. Dados persistem em `~/.local/share/lerd/data/<service>/`

Presets default: mysql, mariadb, postgres, mongo, redis, valkey, meilisearch, elasticsearch, typesense, rustfs, mailpit, gotenberg, memcached, stripe-mock, e os "tooling": pgadmin, phpmyadmin, mongo-express, elasticvue.

## Problemas comuns

### 🔴 `lerd service start mysql` — `Job for lerd-mysql.service failed`

🔍 Diagnóstico:
```bash
journalctl --user -u lerd-mysql.service --since '5 min ago' -n 50 --no-pager
podman logs lerd-mysql 2>&1 | tail -30
```

Causas comuns:
- **Porta já em uso**: outro mysql/mariadb local rodando
- **Volume corrompido**: pasta `~/.local/share/lerd/data/mysql/` com permissões ou meta corrompida
- **Imagem ausente**: pull falhou e o quadlet aponta pra image inexistente

🟢 Conserto:
```bash
# Porta ocupada (porta default 3306):
sudo ss -tlnp | grep 3306
sudo systemctl stop mysql            # ou mariadb

# Volume corrompido (CUIDADO — apaga dados):
systemctl --user stop lerd-mysql
mv ~/.local/share/lerd/data/mysql ~/.local/share/lerd/data/mysql.bak
systemctl --user start lerd-mysql    # cria pasta nova

# Image faltando:
podman pull docker.io/library/mysql:8.4
systemctl --user restart lerd-mysql
```

### 🔴 `lerd db:shell` falha com "Can't connect to MySQL server"

🔍 Diagnóstico:
```bash
podman exec lerd-mysql mysqladmin ping -uroot -plerd
# Espera-se: "mysqld is alive"
```

🟢 Conserto: container está up, mas mysqld interno ainda inicializando. Aguarde:
```bash
until podman exec lerd-mysql mysqladmin ping -uroot -plerd 2>/dev/null | grep -q alive; do sleep 1; done
lerd db:shell
```

### 🔴 Postgres: `psql: error: connection to server ... FATAL: role "lerd" does not exist`

🔍 Diagnóstico:
```bash
podman exec lerd-postgres psql -U postgres -c '\du'
```

🟢 Conserto:
```bash
# Recria role:
podman exec lerd-postgres psql -U postgres -c "CREATE USER lerd WITH SUPERUSER PASSWORD 'lerd';"
```

### 🔴 Service custom (instalado via preset YAML) não inicia

🔍 Diagnóstico:
```bash
lerd service list
cat ~/.config/lerd/services/<servico>.yaml
```

🟢 Conserto:
```bash
lerd service preset show <servico>   # mostra preset embarcado
lerd service reinstall <servico>     # recria quadlet
```

### 🔴 RustFS / MinIO: bucket criado mas Laravel diz "Bucket does not exist"

🔍 Diagnóstico:
```bash
podman exec lerd-rustfs mc ls local/        # lista buckets internos
cat .env | grep AWS_BUCKET                   # nome esperado pelo Laravel
```

🟢 Conserto:
```bash
podman exec lerd-rustfs mc mb local/<nome-bucket>
# Ou recriar via lerd:
lerd env                                     # detecta config S3 do .env e cria
```

### 🔴 Conflito de porta entre versões do mesmo preset (mysql 5.7 vs 8.4)

⚠️ Cada versão alternada tem porta diferente (mysql:8.4 → 3306, mysql:5.7 → 3357, mysql:9.7 → 3397). Confirme no preset YAML qual porta cada uma usa.

🔍 Diagnóstico:
```bash
podman ps --format '{{.Names}}\t{{.Ports}}' | grep mysql
```

🟢 Conserto: usar uma versão de cada vez ou alterar `host_port` no preset.

### 🔴 `lerd service stop mongodb` deixa container running fantasma

🔍 Diagnóstico: `mongodb` é alias do preset `mongo`. Confira nome exato:
```bash
podman ps -a | grep -i mongo
```

🟢 Conserto:
```bash
lerd service stop mongo              # nome canônico, sem 'db'
# Ou direto:
podman rm -f lerd-mongo
```

## ⚠️ Sobre comandos destrutivos

O dashboard intencionalmente **não expõe** botões para `drop database`, `truncate`, `migrate:fresh`, etc. O fork removeu esses one-click dos defaults do Laravel/Symfony porque é muito fácil disparar contra a DB errada (especialmente em projetos Oracle compartilhados).

Pra executar mesmo assim:
```bash
lerd php artisan db:wipe --force        # via CLI, com confirmação
lerd db:export <site>                    # backup ANTES, sempre
```

## 💡 Dicas

- `lerd service status` mostra todos com cor (verde/amarelo/cinza/vermelho).
- `lerd db:export` faz dump SQL; `lerd db:import` restaura. Funciona com qualquer DB do preset.
- `lerd db:isolate` clona a DB do parent na worktree atual (útil pra branch que quer mexer no schema sem afetar a branch principal).
