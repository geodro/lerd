# PHP-FPM — Debug

[← voltar pro índice](../DEBUG.md)

## Como funciona

Uma imagem por versão PHP: `lerd-php74-fpm:local`, `lerd-php83-fpm:local`, …, `lerd-php85-fpm:local`. Cada uma:

- Builder stage compila ~25 extensões + Xdebug + oci8 + Instant Client 21.18 + memcached + amqp
- Runtime stage copia só os `.so` + libs runtime (sem toolchain)
- O hash SHA-256 do template Containerfile vira a tag base no `ghcr.io` (cache pre-built)

```
   docker.io/library/php:X.Y-fpm-alpine
              │
              ▼
   [builder] apk add toolchain → docker-php-ext-install ... → pecl install ... → oci8 + Instant Client
              │
              ▼
   [runtime] apk add runtime libs → COPY .so files from builder → CA mkcert + zsh + composer
              │
              ▼
   lerd-php<short>-fpm:local
              │
              ▼ run as systemd user unit lerd-php<short>-fpm
   nginx → fastcgi → :9000 (php-fpm)
```

## Problemas comuns

### 🔴 `lerd php:rebuild` falhou em `pecl install <ext>`

🔍 Diagnóstico: o log do build mostra exatamente onde quebrou. Geralmente:

| Erro                                              | Causa                                                            |
|---------------------------------------------------|------------------------------------------------------------------|
| `Package "<ext>" does not have REST xml available`| Versão da extensão não suporta o PHP da imagem                   |
| `error: <header>.h: No such file or directory`    | Faltou pacote `-dev` em `apk-deps`                               |
| `make: *** [Makefile:N] Error 1`                  | Bug do source — tentar versão anterior do pacote pecl            |

🟢 Conserto:
```bash
# Pinar versão da extensão (no caso, ssh2 quebrando no 8.3):
lerd php:ext add ssh2-1.4.1 8.3 --apk-deps "libssh2-dev"
```

Ou removê-la se estiver bloqueando o build:
```bash
lerd php:ext remove <ext> 8.3
lerd php:rebuild 8.3
```

### 🔴 Extensão não aparece em `php -m` mesmo após instalar

🔍 Diagnóstico:
```bash
podman run --rm lerd-php85-fpm:local php -m | grep -i <ext>
podman run --rm lerd-php85-fpm:local sh -c 'ls /usr/local/lib/php/extensions/no-debug-non-zts-*/<ext>.so'
podman run --rm lerd-php85-fpm:local sh -c 'cat /usr/local/etc/php/conf.d/*<ext>*.ini'
```

🟢 Conserto: o `.so` existe mas o `docker-php-ext-enable` falhou — a verificação acontece automaticamente após `lerd php:ext add` (rolling back se falhar). Para casos manuais:
```bash
podman exec lerd-php85-fpm sh -c 'docker-php-ext-enable <ext> && kill -USR2 1'
```

### 🔴 `lerd php:ext add <ext>` instala mas FPM volta com 500

🔍 Diagnóstico:
```bash
podman logs lerd-php85-fpm 2>&1 | tail -30
# Procure por "PHP Fatal error" ou "Unable to load dynamic library"
```

🟢 Conserto: provavelmente a extensão precisa de uma lib runtime que foi instalada só no builder. Adicione no `apk-deps` ao instalar — o `lerd php:ext` já replica esses pacotes pro estágio runtime.

### 🔴 `lerd update` puxou nova versão mas a imagem PHP não rebuildou

🔍 Diagnóstico:
```bash
cat ~/.cache/lerd/fpm_image_hash 2>/dev/null    # hash da última build
cat <<'GO' | go run -                            # hash atual do template embarcado
... (não há jeito fácil sem o binário antigo)
GO
```

🟢 Conserto:
```bash
lerd php:rebuild                                # rebuilda todas
lerd php:rebuild 8.5 --local                    # rebuild 8.5 from scratch
```

### 🔴 Xdebug ativado mas IDE não recebe step

🔍 Diagnóstico:
```bash
cat ~/.local/share/lerd/php/8.5/99-xdebug.ini
# Deve ter: xdebug.client_host=host.containers.internal e xdebug.start_with_request=trigger
podman exec lerd-php85-fpm php -i | grep -i xdebug
```

🟢 Conserto:
```bash
# IDE precisa estar escutando na 9003 (não 9000, que é o FPM)
# E o request deve carregar cookie/header XDEBUG_TRIGGER (ou query ?XDEBUG_TRIGGER=1)
# Ou trocar pra start_with_request=yes (sempre liga, mais pesado)
lerd php:ini 8.5                                # editor com validação
```

### 🔴 Composer dentro do container não acha pacote global do host

⚠️ Esperado. O composer-global do container vive em `/root/.composer/`, separado do host. O `lerd composer` sincroniza o `bin/` global automaticamente — outros pacotes globais não.

🟢 Conserto:
```bash
lerd composer global require <pacote>           # instala dentro do container
# Pra usar:
lerd composer global show -i                    # lista o que tem
```

### 🔴 oci8 não carrega após `lerd php:install 8.3` numa máquina nova

Veja [`oracle.md`](oracle.md).

## 💡 Dicas

- `lerd php --ri oci8` (ou outra ext) mostra runtime info: versão, paths, configs ativos.
- `lerd php:list` mostra versões com a default marcada.
- O conteúdo de `~/.local/share/lerd/php/<v>/98-user.ini` sobrescreve o `php.ini` padrão — edite com `lerd php:ini <v>` (tem validador).
- Pra debugar segfault no FPM: `podman exec -it lerd-php85-fpm sh` → instala `gdb` → `apk add gdb` → `gdb php-fpm <pid>`.
