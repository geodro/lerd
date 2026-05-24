# Debug & Troubleshooting — Lerd Oracle Edition

Guia rápido pra desbugar problemas comuns. Em pt-BR, comando exato primeiro, teoria depois.

> **Legenda usada nos guias:**
> 🔴 **Sintoma** · 🔍 **Diagnóstico** · 🟢 **Conserto** · ⚠️ **Atenção** · 💡 **Dica**

## Mapa rápido: sintoma → onde olhar

| Sintoma                                                  | Guia                                                  |
|----------------------------------------------------------|-------------------------------------------------------|
| Site retorna `502 Bad Gateway`                           | [nginx](debug/nginx.md), [php-fpm](debug/php-fpm.md)  |
| `meusite.localhost` (ou `.test`) não resolve             | [dns](debug/dns.md)                                   |
| Container/unit do PHP-FPM falha ao subir                 | [podman](debug/podman.md), [php-fpm](debug/php-fpm.md)|
| `lerd php:rebuild` falha no `pecl install <ext>`         | [php-fpm](debug/php-fpm.md)                           |
| `ORA-12541` / `ORA-12154` / `ORA-01017` no Laravel       | [oracle](debug/oracle.md)                             |
| `oci_connect` retorna `false` sem mensagem clara         | [oracle](debug/oracle.md)                             |
| `lerd link` registrou o site mas o vhost não foi gerado  | [sites](debug/sites.md)                               |
| `lerd-mysql`/`lerd-postgres` não inicia / porta ocupada  | [services](debug/services.md)                         |
| Worker (`lerd-*-queue`, `*-horizon`) não sobe ou loop    | [workers](debug/workers.md)                           |
| `lerd update` falhou no meio do caminho                  | [updates](debug/updates.md)                           |
| Browser não confia no HTTPS local (mkcert)               | [nginx](debug/nginx.md)                               |
| IPv6 entre containers retornando `connection refused`    | [podman](debug/podman.md), [dns](debug/dns.md)        |

## Antes de qualquer coisa

```bash
lerd doctor             # bateria de checagens (DNS, podman, certs, services)
lerd about              # versão e commit instalados
podman ps -a            # estado dos containers (incluindo os parados)
journalctl --user -u 'lerd-*' --since '5 min ago' --no-pager | tail -50
```

Esses 4 comandos resolvem ou pelo menos diagnosticam ~70% dos problemas.

## Guias por tópico

- **[Podman](debug/podman.md)** — rootless, network `lerd`, quadlets systemd, restart cascades
- **[Nginx](debug/nginx.md)** — 502 ao FPM, mkcert CA, vhost regen, lan-share
- **[DNS](debug/dns.md)** — `.localhost` vs `.test`, NSS resolver, dnsmasq, IPv6
- **[PHP-FPM](debug/php-fpm.md)** — image hash mismatch, extensões que não carregam, `php.ini` por versão
- **[Oracle / oci8](debug/oracle.md)** — códigos `ORA-*`, Instant Client paths, `NLS_LANG`, charset, gcompat shim
- **[Sites e link](debug/sites.md)** — `.lerd.yaml`, drift do `sites.yaml`, detecção de framework
- **[Services (DB/cache/etc)](debug/services.md)** — quadlets de mysql/postgres/redis, conflitos de porta
- **[Workers](debug/workers.md)** — queue/horizon/schedule/reverb sob systemd user
- **[Updates do fork](debug/updates.md)** — `lerd update`, versionamento `-oracle.N`, rollback

## Última cartada: bug report

Quando você está em loop e nada funciona, gere um relatório completo pra abrir issue:

```bash
lerd bug-report                                          # gera arquivo em /tmp/
# Ou direto da clipboard (dashboard → System → Debug → "Copiar relatório")
```

Issue em: <https://github.com/gabriel-sousa99/lerd/issues>
