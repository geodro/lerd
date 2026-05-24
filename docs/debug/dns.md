# DNS — Debug

[← voltar pro índice](../DEBUG.md)

## Como funciona

O fork Oracle tem dois modos de DNS — você escolhe no `lerd install`:

| Modo                | TLD padrão  | Como resolve                                              | Precisa sudo? |
|---------------------|-------------|-----------------------------------------------------------|---------------|
| **opt-out** (default) | `.localhost` | RFC 6761 — qualquer SO moderno resolve `*.localhost` → loopback | Não |
| **gerenciado**      | `.test`     | Container `lerd-dns` (dnsmasq) escuta na 53; NSS aponta pra ele | Sim (uma vez) |

```
       opt-out (.localhost):
              browser → SO resolver → ::1 / 127.0.0.1
                                            ▲
                                            │ nada que fazer no resolver
                                            │ kernel/glibc faz sozinho

       gerenciado (.test):
              browser → SO resolver → /etc/resolv.conf
                                              │
                                              ▼
                                       127.0.0.53 (systemd-resolved)
                                              │
                                              ▼  via NSS / DNSSEC-NTA / Plugin
                                       lerd-dns container (dnsmasq)
                                              │
                                              ├── *.test → 127.0.0.1
                                              └── outros → forward upstream
```

## Problemas comuns

### 🔴 `meusite.localhost` não resolve

⚠️ É raro porque é uma RFC. Mas acontece quando:
- Você forçou `nss-resolve` puro sem fallback pra `nss-files`
- Algum proxy corporativo intercepta DNS
- Browser interpretou como search engine query (sem `http://` na frente)

🔍 Diagnóstico:
```bash
getent hosts meusite.localhost                  # deve dar 127.0.0.1 ou ::1
ping -c 1 meusite.localhost
cat /etc/nsswitch.conf | grep ^hosts            # files deve aparecer antes de dns
```

🟢 Conserto:
```bash
# Garantir entrada explicit (raramente necessário):
echo '127.0.0.1  *.localhost' | sudo tee -a /etc/hosts
# Ou no browser: sempre digitar com http:// na frente.
```

### 🔴 `meusite.test` não resolve, modo DNS gerenciado

🔍 Diagnóstico:
```bash
lerd dns:check                                  # bateria de checagens em camadas
systemctl --user is-active lerd-dns
cat /etc/systemd/resolved.conf.d/lerd.conf 2>/dev/null
```

🟢 Conserto:
```bash
lerd install --from-update                      # reinstala plugin do resolver
sudo systemctl restart systemd-resolved
# Em fedora/nobara, garantir que NetworkManager não esteja sobrescrevendo:
sudo cat /etc/NetworkManager/conf.d/*.conf | grep -E 'dns='
```

### 🔴 IPv6 falha (`::1` ok mas `fd...` containers entre si não)

🔍 Diagnóstico:
```bash
podman exec lerd-php85-fpm getent hosts lerd-mysql
# Espera-se output IPv4 e IPv6 (fd00:...)
sysctl net.ipv6.conf.all.disable_ipv6           # deve ser 0
```

🟢 Conserto: se `disable_ipv6=1`, lerd setup teve que pular IPv6.
```bash
echo 'net.ipv6.conf.all.disable_ipv6 = 0' | sudo tee /etc/sysctl.d/99-lerd-ipv6.conf
sudo sysctl --system
lerd install --from-update
```

### 🔴 Após instalar lerd-dns, perdeu acesso à internet

⚠️ Aconteceu se `lerd-dns` foi configurado mas o container caiu. O resolver passou a apontar pra um listener inexistente.

🟢 Conserto rápido:
```bash
sudo rm /etc/systemd/resolved.conf.d/lerd.conf
sudo systemctl restart systemd-resolved
# Depois traga o lerd-dns de volta:
systemctl --user start lerd-dns
```

### 🔴 `*.localhost` resolve mas porta 80 fica como `Connection refused`

🔍 Diagnóstico:
```bash
ss -tlnp | grep -E ':80\b'
podman ps | grep lerd-nginx
```

🟢 Conserto:
```bash
systemctl --user start lerd-nginx
```

## Cliente VPN corporativa quebra `.localhost`?

Algumas VPNs corporativas reescrevem todo o DNS. Soluções:

1. **Excluir `*.localhost`** das configs DNS da VPN (busque "split DNS" ou "DNS exception" na sua VPN).
2. **Subir lerd-dns** localmente e apontar `*.localhost` pra `127.0.0.1` na config local da VPN.
3. **Trocar pra modo `.test`** (`lerd install` e responder "sim" ao DNS gerenciado) — algumas VPNs respeitam `.test` por ser RFC 2606.

## 💡 Dicas

- `lerd dns:check` é o comando-canivete: roda tudo (`getent`, `dig`, `ss`, `nss`) e mostra resultado em camadas.
- O containerd Podman tem DNS interno só pra containers da mesma network. Por isso `lerd-php85-fpm` consegue resolver `lerd-mysql`, mas o host não.
- `host.containers.internal` resolve pro IP do gateway da network — útil pra acessar serviços que rodam direto no host (não em container).
