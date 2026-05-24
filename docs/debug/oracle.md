# Oracle / oci8 — Debug

[← voltar pro índice](../DEBUG.md)

## Como funciona

Toda imagem PHP do fork tem:

- **Oracle Instant Client 21.18** em `/opt/oracle/instantclient_21_18`, com symlink em `/opt/oracle/instantclient`
- **oci8** compilado via PECL: 2.2.0 (PHP 7.x), 3.0.1 (8.0), 3.3.0 (8.1/8.2/8.3), 3.4.x (8.4+)
- `ENV LD_LIBRARY_PATH=/opt/oracle/instantclient` + `ENV ORACLE_HOME=/opt/oracle/instantclient`
- **gcompat + libc6-compat** (Alpine é musl; Instant Client é glibc — precisa do shim)
- **libaio, libnsl, libstdc++** (deps diretos de `libclntsh.so`)

```
   php artisan migrate
        │
        ▼
   PDO Oracle (yajra/laravel-oci8) ─→ ext/oci8.so
                                          │
                                          ▼ dlopen via LD_LIBRARY_PATH
                              /opt/oracle/instantclient/libclntsh.so.21.1
                                          │ (glibc-linked, rodando via gcompat)
                                          ▼ TNS protocol
                                  Oracle server (porta 1521)
```

## Códigos `ORA-*` comuns

| Código        | Significado                                       | Causa frequente                                     |
|---------------|---------------------------------------------------|-----------------------------------------------------|
| `ORA-12541`   | TNS:no listener                                   | Servidor inalcançável ou listener offline           |
| `ORA-12154`   | Could not resolve the connect identifier          | Service name errado no `DB_DATABASE`                |
| `ORA-12545`   | Connect failed: target host or object does not exist | Host inválido ou firewall                       |
| `ORA-12170`   | TNS:Connect timeout occurred                      | Latência alta / firewall com drop                   |
| `ORA-01017`   | invalid username/password; logon denied           | Credenciais erradas                                 |
| `ORA-00942`   | table or view does not exist                      | Permissão ou schema errado (não é erro de conexão!) |
| `ORA-28000`   | the account is locked                             | Conta bloqueada por tentativas erradas              |
| `ORA-28001`   | the password has expired                          | Senha expirou — DBA precisa resetar                 |

## Problemas comuns

### 🔴 `oci_connect()` retorna `false` sem mensagem

🔍 Diagnóstico:
```bash
lerd php -r 'oci_connect("u","p","host:1521/SERVICE"); print_r(oci_error());'
# A função estática oci_error() lê o último erro do thread, mesmo sem connection handle
```

🟢 Conserto comum:
```bash
# Confirmar Instant Client carregando:
lerd php --ri oci8 | head -8
# Deve mostrar: "Oracle Run-time Client Library Version => 21.18.0.0.0"
```

### 🔴 `OCIEnvNlsCreate() failed. There is something wrong with your system - please check that ORACLE_HOME ...`

🔍 Diagnóstico:
```bash
podman exec lerd-php85-fpm sh -c 'echo $LD_LIBRARY_PATH && ls -la /opt/oracle/instantclient/libclntsh*'
```

🟢 Conserto:
```bash
# Imagem antiga sem Instant Client. Forçar rebuild local (sem pull):
lerd php:rebuild 8.5 --local
```

### 🔴 `ORA-12541` ao conectar contra Oracle local de teste (gvenzl/oracle-xe)

🔍 Diagnóstico:
```bash
podman ps | grep oracle
podman logs <oracle-container> 2>&1 | tail -20 | grep -i "DATABASE IS READY"
podman inspect <oracle-container> --format '{{.NetworkSettings.Ports}}'
```

🟢 Conserto:
```bash
# Se o Oracle XE não está pronto, esperar:
until podman logs <oracle-container> 2>&1 | grep -q 'DATABASE IS READY'; do sleep 2; done

# Se está na porta certa mas não chega: o container do PHP precisa estar no mesmo network OU usar --network=host:
lerd php artisan migrate                              # usa rede lerd interna
podman run --rm --network=host lerd-php85-fpm:local <cmd>   # usa rede host (acessa 127.0.0.1:1521)
```

### 🔴 Acentuação portuguesa virou `?` ou caracteres trocados

🔍 Diagnóstico:
```bash
lerd php -r 'echo getenv("NLS_LANG");'                # vazio = default = US7ASCII
```

🟢 Conserto: edite o `.lerd.yaml` do projeto:
```yaml
oracle:
  charset: AL32UTF8                                   # ou WE8MSWIN1252 (Windows-1252) ou WE8ISO8859P15 (Latin-1+€)
```
Depois:
```bash
lerd env                                              # reescreve DB_CHARSET e NLS_LANG no .env
lerd restart
```

### 🔴 `ORA-21561` ao usar `oci_pconnect` com persistência

⚠️ Bug conhecido do Instant Client 21.x quando combina connection pooling com SELinux ativo. Não acontece no fork porque os quadlets já têm `--security-opt=label=disable`. Se persistir:
```bash
podman exec lerd-php85-fpm sh -c 'getsebool -a 2>/dev/null | grep -i container'
```

### 🔴 Connection lenta (segundos por query) mesmo em rede local

🔍 Diagnóstico: provavelmente DNS resolution dentro do TNS.
```bash
podman exec lerd-php85-fpm sh -c 'nslookup <oracle-host>; time nslookup <oracle-host>'
```

🟢 Conserto: usar IP direto em vez de hostname no `.lerd.yaml`/`.env`, ou adicionar entrada explícita em `/etc/hosts` do host (montado nos containers).

## 💡 Dicas

- Pra um Oracle de teste rápido (sem servidor corporativo):
  ```bash
  podman run -d --name oracle-test -p 1521:1521 \
    -e ORACLE_PASSWORD=lerd -e ORACLE_DATABASE=LERDPDB \
    -e APP_USER=lerd_app -e APP_USER_PASSWORD=lerd \
    docker.io/gvenzl/oracle-xe:21-slim-faststart
  ```
  Aguarde "DATABASE IS READY" no `podman logs`.

- Para verificar do host: `podman exec oracle-test sqlplus -S lerd_app/lerd@//127.0.0.1:1521/LERDPDB <<<"SELECT 1 FROM DUAL;"`.

- O `yajra/laravel-oci8` (Laravel) e `doctrine/dbal` com driver `oci8` ambos funcionam — o driver baixo é o mesmo, só muda a camada do framework.
