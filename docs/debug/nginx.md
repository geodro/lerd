# Nginx — Debug

[← voltar pro índice](../DEBUG.md)

## Como funciona

Um único container `lerd-nginx` faz reverse-proxy de TODOS os sites. Para cada site `lerd link`-ado, é gerado um vhost em `~/.local/share/lerd/nginx/sites/<dominio>.conf` que define o `upstream` apontando para a unidade FPM da versão PHP do projeto:

```
       browser ─┐
                ▼
       lerd-nginx  (porta 80/443 no host)
                │
                │  upstream = "fastcgi://lerd-php85-fpm:9000"
                │  (resolução via DNS interno do Podman)
                ▼
       lerd-php85-fpm  (php-fpm rodando como root no container)
                │
                └─ bind mount /home/gabriel:/home/gabriel:rw
                     ▲
                     │   o código do projeto vive no host
                     │   o container roda o php-fpm em cima dele
```

HTTPS: certificados gerados via mkcert (a CA local fica em `/usr/local/share/ca-certificates/mkcert-ca.crt` dentro de cada imagem FPM, registrada na cadeia do Alpine).

## Problemas comuns

### 🔴 `502 Bad Gateway` em todos os sites

🔍 Diagnóstico:
```bash
systemctl --user is-active lerd-nginx          # nginx ok?
systemctl --user is-active lerd-php85-fpm      # FPM da versão do projeto ok?
podman exec lerd-nginx nginx -t                # config válido?
```

🟢 Conserto: o FPM provavelmente caiu. Veja [`php-fpm.md`](php-fpm.md) ou:
```bash
systemctl --user restart lerd-php85-fpm
lerd restart                                    # restart do site atual
```

### 🔴 `502 Bad Gateway` em UM site específico

🔍 Diagnóstico:
```bash
cat ~/.local/share/lerd/nginx/sites/<dominio>.conf | grep fastcgi_pass
# Deve apontar pra lerd-php<X>-fpm:9000 — a versão correta do projeto
podman exec lerd-nginx ping -c 1 lerd-php85-fpm
```

🟢 Conserto: o vhost ficou apontando pra uma versão PHP que não está rodando.
```bash
cd ~/projeto
lerd link                                       # regenera o vhost do site atual
```

### 🔴 `403 Forbidden` em todos os sites recém-criados

🔍 Diagnóstico:
```bash
ls -la ~/projeto/public      # ou public_html/, dependendo do framework
# As permissões precisam permitir leitura pelo www-data dentro do container
```

🟢 Conserto: o container roda como root (`user=root` no `[www]`), então o problema NÃO é permissão. Geralmente é `public_dir` errado:
```bash
cd ~/projeto
cat .lerd.yaml | grep public_dir
# Se o framework usa "public_html/" mas .lerd.yaml diz "public/", ajuste.
lerd link                                       # rebuilda o vhost
```

### 🔴 HTTPS quebrado / browser não confia

🔍 Diagnóstico:
```bash
mkcert -CAROOT                                  # mostra onde a CA está
ls -la "$(mkcert -CAROOT)/rootCA.pem"
# Verificar se a CA está instalada no truststore do sistema:
trust list | grep mkcert
```

🟢 Conserto:
```bash
mkcert -install                                 # instala CA no truststore (e nos browsers)
lerd php:rebuild                                # rebuilda imagens com CA atualizada
# E reinicie o browser inteiramente (Chrome cacheia CA por sessão)
```

### 🔴 `Address already in use` ao subir lerd-nginx (porta 80 ou 443)

🔍 Diagnóstico:
```bash
sudo ss -tlnp | grep -E ':(80|443)\b'
# Comum: apache2, httpd, ou um outro nginx do sistema
```

🟢 Conserto:
```bash
sudo systemctl stop apache2  # ou httpd, nginx
sudo systemctl disable apache2
systemctl --user restart lerd-nginx
```

### 🔴 `lerd lan` / `lerd lan-share` não acessível de outro dispositivo

🔍 Diagnóstico:
```bash
ip a | grep -E 'inet .* (eth|wlan|enp|wlp)' | awk '{print $2}'
# Anote o IP. No outro device, http://<IP>:<lan-port>
sudo firewall-cmd --list-ports                  # firewalld
sudo ufw status                                 # ufw
```

🟢 Conserto:
```bash
# firewalld
sudo firewall-cmd --add-port=80/tcp --permanent && sudo firewall-cmd --reload
# ufw
sudo ufw allow 80/tcp
```

## 💡 Dicas

- O nginx do lerd loga em `journalctl --user -u lerd-nginx`. Sem nada no `/var/log/nginx/`.
- Pra ver o vhost gerado: `podman exec lerd-nginx cat /etc/nginx/sites-enabled/<dominio>.conf`.
- `lerd open` abre o site no browser usando o esquema (http/https) correto do `.lerd.yaml`.
- Após mudar `secured` no `.lerd.yaml`, rode `lerd secure` / `lerd unsecure` pra forçar a regeneração do cert.
