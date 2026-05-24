# Lerd Oracle Edition

> Fork do [`geodro/lerd`](https://github.com/geodro/lerd) com **suporte a
> Oracle Database embutido em todas as imagens PHP** — Oracle Instant
> Client 21.18 (LTS) + `oci8` + memcached + amqp já compilados, prontos
> para PHP 7.4 → 8.5. Drop-in replacement: todo comando `lerd` existente
> funciona igual.

> [!IMPORTANT]
> Este fork mantém o mesmo binário `lerd` (compatibilidade total) e
> aponta o auto-update para **este** repositório em vez do upstream.
> Releases publicadas aqui seguem o esquema `v1.21.2-oracle.N`.

[![Fork base](https://img.shields.io/badge/forked%20from-geodro%2Flerd%20v1.21.2-blue)](https://github.com/geodro/lerd)
[![Oracle Instant Client](https://img.shields.io/badge/Oracle%20Instant%20Client-21.18-red)]()
[![PHP](https://img.shields.io/badge/PHP-7.4%20%E2%80%93%208.5-777BB4)]()

---

## Sumário

- [O que muda em relação ao upstream](#o-que-muda-em-relação-ao-upstream)
- [Instalação](#instalação)
- [Primeiro uso](#primeiro-uso)
- [Trabalhando com Oracle](#trabalhando-com-oracle)
- [Serviços (start, stop, status)](#serviços-start-stop-status)
- [Rebuild de imagens PHP](#rebuild-de-imagens-php)
- [Atualização](#atualização)
- [Desinstalação](#desinstalação)
- [Diagnóstico](#diagnóstico)
- [Debug e troubleshooting](#debug-e-troubleshooting)
- [Lista de comandos](#lista-de-comandos-úteis)

---

## O que muda em relação ao upstream

| Recurso                       | Upstream `geodro/lerd` | Esta fork                        |
|-------------------------------|------------------------|----------------------------------|
| Driver Oracle (`oci8`)        | precisa instalar manual | **já vem compilado** em toda imagem |
| Oracle Instant Client         | n/a                    | **21.18 LTS** no PATH (`/opt/oracle/instantclient`) |
| Extensão `memcached`          | precisa `lerd php:ext` | **pré-instalada**                |
| Extensão `amqp` (RabbitMQ)    | precisa `lerd php:ext` | **pré-instalada**                |
| `lerd init` → "Database"      | sqlite / mysql / postgres | **+ Oracle (externo)**         |
| `.lerd.yaml` → bloco `oracle:`| ✗                      | **host/port/service/user/pass**  |
| DNS padrão                    | `lerd-dns` + `.test` (precisa sudo no dnsmasq) | **off, `.localhost`** (resolve em qualquer SO) |
| Auto-update                   | `geodro/lerd/releases` | `gabriel-sousa99/lerd/releases`  |
| Versão                        | `1.21.2`               | `1.21.2-oracle.N`                |

### Lista completa de extensões PHP nas imagens

> 25 extensões compiladas no builder + 6 via PECL — cobre o ecossistema
> top-10 Laravel (Sanctum, Horizon, Telescope, spatie/* , Filament,
> Socialite, Livewire, Debugbar) sem `lerd php:ext add`.

```
oci8        memcached  amqp        redis      imagick    mongodb
igbinary    pcov       xdebug      spx        opcache
curl        gd         intl        zip        pdo_mysql  pdo_pgsql
mysqli      soap       xsl         ldap       pcntl      exif
bcmath      mbstring   gmp         bz2        sysv*      sockets
calendar    dba        shmop       (e tudo que o php:X.Y-fpm-alpine já traz)
```

---

## Instalação

### Pré-requisitos

- Linux (Arch, Fedora/Nobara, Debian/Ubuntu, openSUSE) ou macOS
- **Podman 4+** (rootless, sem Docker)
- `git`, `curl` ou `wget`

### Instalar

```bash
curl -fsSL https://raw.githubusercontent.com/gabriel-sousa99/lerd/main/install.sh | bash
```

ou via `wget`:

```bash
wget -qO- https://raw.githubusercontent.com/gabriel-sousa99/lerd/main/install.sh | bash
```

O script:

1. Verifica `podman`, `git`, `mkcert` (instala via gerenciador do seu distro se faltar)
2. Baixa o binário pré-compilado para `$HOME/.local/bin/lerd`
3. Pergunta se quer ativar DNS gerenciado pelo lerd
   - **Padrão neste fork: NÃO** → seus sites ficam em `http://meusite.localhost/`
   - Sim → instala `lerd-dns` (dnsmasq containerizado, sites em `meusite.test`)

> [!TIP]
> Sem sudo, sem mexer no resolver do sistema. `*.localhost` resolve para
> loopback em qualquer SO moderno (RFC 6761).

### Verificar instalação

```bash
lerd about
# Deve exibir: "Lerd Oracle Edition" e versão 1.21.2-oracle.N
```

---

## Primeiro uso

Dentro de qualquer projeto PHP:

```bash
cd ~/meu-projeto-laravel
lerd init     # wizard interativo (PHP, DB, HTTPS, serviços, workers)
lerd link     # registra o site (já chamado pelo init)
lerd open     # abre no navegador em http://meu-projeto-laravel.localhost
```

O wizard pergunta:

- **Versão do PHP** (7.4, 8.0, 8.1, 8.2, 8.3, 8.4, 8.5)
- **Versão do Node** (22, 24)
- **HTTPS?** (S/N — cria certificado via mkcert)
- **Database**:
  - SQLite (sem container)
  - MySQL (lerd-mysql)
  - PostgreSQL (lerd-postgres)
  - **Oracle (externo, oci8 já embutido)** ← novo
- **Serviços extras** (Redis, Meilisearch, RustFS/S3, Mailpit, Gotenberg, etc.)

Ao escolher **Oracle**, abre sub-form pedindo:

- Host (ex: `oracle.example.com`)
- Porta (padrão `1521`)
- Service name / SID (ex: `XEPDB1`, `ORCLPDB1`)
- Usuário
- Senha

Os valores vão para `.lerd.yaml` em um bloco `oracle:` e o `.env` recebe
`DB_CONNECTION=oracle` + `DB_HOST` + `DB_PORT` + `DB_DATABASE` (= service
name) + `DB_USERNAME` + `DB_PASSWORD`. Lerd não tenta subir container
Oracle (é DB externo).

---

## Trabalhando com Oracle

### Cliente Laravel: instalar `yajra/laravel-oci8`

```bash
lerd composer require yajra/laravel-oci8
lerd php artisan vendor:publish --provider="Yajra\Oci8\Oci8ServiceProvider" --tag=oracle --force
```

### Confirmar que `oci8` carregou

```bash
lerd php -r 'var_dump(extension_loaded("oci8"));'
# bool(true)
lerd php --ri oci8 | head -6
#   OCI8 Version => 3.4.1
#   Oracle Run-time Client Library Version => 21.18.0.0.0
```

### Subir um Oracle de teste local (Oracle XE 21)

Para validação sem depender do servidor corporativo:

```bash
podman run -d --name lerd-oracle-test \
  -p 1521:1521 \
  -e ORACLE_PASSWORD=lerd \
  -e ORACLE_DATABASE=LERDPDB \
  -e APP_USER=lerd_app \
  -e APP_USER_PASSWORD=lerd \
  docker.io/gvenzl/oracle-xe:21-slim-faststart
```

Esperar ~30s (`podman logs -f lerd-oracle-test`) até "DATABASE IS READY".
Depois configure o `.env` do seu projeto:

```dotenv
DB_CONNECTION=oracle
DB_HOST=127.0.0.1
DB_PORT=1521
DB_DATABASE=LERDPDB
DB_USERNAME=lerd_app
DB_PASSWORD=lerd
DB_CHARSET=AL32UTF8
```

E rode:

```bash
lerd php artisan migrate
```

### Charset / NLS_LANG

O `.lerd.yaml` aceita um campo opcional `charset:` no bloco oracle.
Quando definido, `lerd env` escreve `DB_CHARSET` + `NLS_LANG`:

```yaml
oracle:
  host: oracle.example.com
  port: 1521
  service_name: PRODPDB
  username: app_user
  password: ${ORACLE_PASSWORD}   # use placeholder e set no shell
  charset: AL32UTF8              # ou WE8MSWIN1252, WE8ISO8859P15
```

---

## Serviços (start, stop, status)

Lerd gerencia serviços via systemd user units. Comandos universais:

```bash
lerd service start <nome>      # ex: lerd service start mysql
lerd service stop <nome>
lerd service restart <nome>
lerd service status            # lista todos com estado
lerd service list              # nomes de presets disponíveis
```

Presets padrão: `mysql`, `mariadb`, `postgres`, `mongo`, `redis`,
`meilisearch`, `elasticsearch`, `rustfs`, `mailpit`, `gotenberg`,
`memcached`, `mongo-express`, `pgadmin`, `phpmyadmin`, etc.

### Parar tudo

```bash
lerd quit          # para containers + UI + watcher + tray
```

### Pausar um único site (mantém serviços)

```bash
cd ~/meu-projeto
lerd pause         # nginx vhost vira landing page
lerd unpause       # retoma
```

---

## Rebuild de imagens PHP

Quando você muda a Containerfile ou quer forçar reconstrução:

```bash
lerd php:rebuild              # rebuilda todas versões instaladas
lerd php:rebuild 8.4          # só a 8.4
lerd php:rebuild --local      # constrói tudo do zero (sem pull do ghcr)
```

> [!NOTE]
> Como o template Containerfile deste fork difere do upstream, o hash
> SHA-256 dos pulls do `ghcr.io/geodro/lerd-php<X>-fpm-base` **não bate**
> e o lerd cai automaticamente no build local — é o comportamento correto
> e garante que suas customizações (Instant Client, oci8, memcached, amqp)
> fiquem na imagem.

### Adicionar uma extensão por cima

Para uma única extensão extra (ex: `pdo_dblib`), continue usando:

```bash
lerd php:ext add pdo_dblib 8.4 --apk-deps "freetds-dev"
lerd php:ext list
lerd php:ext remove pdo_dblib 8.4
```

---

## Atualização

```bash
lerd update                # busca último release em gabriel-sousa99/lerd
lerd update --beta         # se houver pre-release marcada
lerd update --rollback     # volta para a versão anterior (backup automático)
```

O `lerd update` deste fork:

1. Consulta `https://github.com/gabriel-sousa99/lerd/releases/latest`
2. Compara versão atual com `1.21.2-oracle.N`
3. Faz download + substituição atômica do binário em `~/.local/bin/lerd`
4. Roda `lerd install --from-update` para reaplicar quadlets/DNS/sysctl
5. Se a Containerfile mudou: roda `lerd php:rebuild`

> [!WARNING]
> **Não** use `lerd-installer --update` apontado para o upstream — ele
> sobrescreve o binário com a versão sem suporte Oracle.

---

## Desinstalação

```bash
lerd-installer --uninstall
```

O script remove:

- `~/.local/bin/lerd`, `~/.local/bin/lerd-tray`, `~/.local/bin/lerd-installer`
- Unidades systemd user em `~/.config/systemd/user/lerd-*`
- Diretórios `~/.config/lerd/`, `~/.cache/lerd/`, `~/.local/share/lerd/`

> [!CAUTION]
> Os dados dos serviços (MySQL, Postgres, MinIO/RustFS) ficam em
> `~/.local/share/lerd/data/` e são apagados pelo `--uninstall`. Faça
> backup antes (`lerd db:export <site>` por exemplo).

Para limpar imagens podman também:

```bash
podman ps -a --filter "name=lerd-" -q | xargs podman rm -f
podman images --filter "reference=lerd-*" -q | xargs podman rmi -f
podman images --filter "reference=ghcr.io/geodro/lerd-*" -q | xargs podman rmi -f
```

---

## Diagnóstico

```bash
lerd doctor               # verifica DNS, podman, certs, services, versão
lerd bug-report           # gera arquivo com tudo necessário pra abrir issue
lerd dns:check            # diagnóstico em camadas do resolver
lerd logs <site>          # logs do FPM ou nginx do projeto atual
lerd logs <service>       # ex: lerd logs mysql
```

---

## Debug e troubleshooting

Quando algo quebra, comece por:

```bash
lerd doctor                    # bateria de checagens (DNS, podman, certs, services)
lerd dns:check                 # diagnóstico em camadas do resolver
lerd bug-report                # gera arquivo .tar.gz com tudo necessário pra abrir issue
```

Guias por tópico em [`docs/DEBUG.md`](docs/DEBUG.md):

| Sintoma                                    | Onde olhar                                                             |
|--------------------------------------------|------------------------------------------------------------------------|
| Site retorna `502 Bad Gateway`             | [`debug/nginx.md`](docs/debug/nginx.md) + [`debug/php-fpm.md`](docs/debug/php-fpm.md) |
| `.localhost` ou `.test` não resolve        | [`debug/dns.md`](docs/debug/dns.md)                                    |
| Quadlet/systemd falha ao subir             | [`debug/podman.md`](docs/debug/podman.md)                              |
| `ORA-12541` / `ORA-12154` / `ORA-01017`    | [`debug/oracle.md`](docs/debug/oracle.md)                              |
| `lerd update` quebrou                      | [`debug/updates.md`](docs/debug/updates.md)                            |
| Worker em loop / fila parou                | [`debug/workers.md`](docs/debug/workers.md)                            |
| Conflito de porta em MySQL/Postgres        | [`debug/services.md`](docs/debug/services.md)                          |

Também acessível direto pelo dashboard em **System → Debug & Troubleshoot**, com botões pra rodar os diagnósticos e copiar o relatório.

## Lista de comandos úteis

| Comando                                | O que faz                                                  |
|----------------------------------------|------------------------------------------------------------|
| `lerd init`                            | Wizard interativo cria `.lerd.yaml`                        |
| `lerd link`                            | Registra o diretório como site                             |
| `lerd unlink`                          | Remove o site (sem deletar arquivos)                       |
| `lerd open`                            | Abre o site no navegador                                   |
| `lerd dashboard`                       | Abre o painel web (Cmd+K, live widgets)                    |
| `lerd tui`                             | Painel terminal estilo btop                                |
| `lerd php <args>`                      | Roda php no container do projeto (ex: `lerd php artisan tinker`) |
| `lerd composer <args>`                 | Roda composer com binários `composer-global` no PATH       |
| `lerd npm <args>` / `lerd npx <args>`  | Usa Node do projeto via fnm                                |
| `lerd db:shell`                        | Abre shell do DB do projeto                                |
| `lerd db:export` / `lerd db:import`    | Backup / restore                                           |
| `lerd db:isolate`                      | DB próprio para o worktree atual (clone do parent ou vazio)|
| `lerd horizon:start` / `:stop`         | Laravel Horizon como serviço systemd                       |
| `lerd queue:start` / `:stop`           | Worker de filas                                            |
| `lerd schedule:start` / `:stop`        | `php artisan schedule:work`                                |
| `lerd reverb:start` / `:stop`          | WebSocket server (Laravel Reverb)                          |
| `lerd lan`                             | Expõe sites para o LAN                                     |
| `lerd remote-control`                  | Liga/desliga acesso ao dashboard via LAN                   |
| `lerd mcp:enable-global`               | Registra MCP server para Claude / IDE / agentes            |

---

## Compilando do código

```bash
git clone https://github.com/gabriel-sousa99/lerd.git
cd lerd
make build              # binário em build/lerd
make install            # copia para ~/.local/bin/
make test               # go test ./...
```

Requisitos: Go 1.25+, Node 22+, npm 10+.

---

## Créditos

- **Lerd** original — [George Dumitrescu](https://github.com/geodro) ([geodro/lerd](https://github.com/geodro/lerd))
- **Suporte Oracle** (este fork) — [Gabriel Sousa](https://github.com/gabriel-sousa99)

Licença: MIT (herdada do upstream — ver [`LICENSE`](LICENSE)).
