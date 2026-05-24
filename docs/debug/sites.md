# Sites & Link — Debug

[← voltar pro índice](../DEBUG.md)

## Como funciona

Cada projeto tem um `.lerd.yaml` (committado no repo, define PHP/Node/services/workers) e é registrado no `sites.yaml` global (`~/.config/lerd/sites.yaml`, per-machine):

```
   projeto/                            ~/.config/lerd/sites.yaml
   ├── .lerd.yaml      ←─── compartilhado entre máquinas (commitado)
   ├── composer.json                   - name: meusite
   ├── public/                         path: /home/gabriel/projetos/meusite
   └── ...                             domains: [meusite.localhost]
                                       php_version: 8.5
                                       secured: false
                                       framework: laravel
```

O `lerd link` faz três coisas:
1. Lê `.lerd.yaml` (cria se não existir via wizard)
2. Append no `sites.yaml`
3. Gera vhost em `~/.local/share/lerd/nginx/sites/<dominio>.conf`

## Problemas comuns

### 🔴 `lerd link` registra mas o site não aparece em `lerd dashboard`

🔍 Diagnóstico:
```bash
cat ~/.config/lerd/sites.yaml | grep -A 5 "name: meusite"
ls ~/.local/share/lerd/nginx/sites/ | grep meusite
podman exec lerd-nginx ls /etc/nginx/sites-enabled/ | grep meusite
```

🟢 Conserto:
```bash
cd /caminho/do/projeto
lerd unlink                         # remove
lerd link                           # registra de novo
systemctl --user restart lerd-nginx
```

### 🔴 Framework detectado errado

🔍 Diagnóstico:
```bash
cd /caminho/do/projeto
cat .lerd.yaml | grep framework
```

🟢 Conserto: override manual no `.lerd.yaml`:
```yaml
framework: laravel       # ou symfony, wordpress, drupal, cakephp, statamic, none
```

Lerd detecta via `composer.json` (require ou require-dev) e arquivos marcadores. Pra forçar nenhum framework: `framework: none`.

### 🔴 `lerd init` re-roda o wizard mesmo com `.lerd.yaml` existindo

🔍 Diagnóstico: o wizard só pula quando o arquivo existe. Se está rodando, o arquivo não foi salvo no diretório certo.
```bash
ls -la .lerd.yaml
pwd                                 # confirme que está na raiz do projeto
```

🟢 Conserto:
```bash
lerd init --fresh                   # pula o wizard se já existe
```

### 🔴 Site fica em `paused` mesmo após `lerd unpause`

🔍 Diagnóstico:
```bash
ls ~/.local/share/lerd/nginx/sites/<dominio>.conf
head -5 ~/.local/share/lerd/nginx/sites/<dominio>.conf      # se for landing page, ainda está paused
```

🟢 Conserto:
```bash
lerd unpause
lerd link                           # regenera vhost
```

### 🔴 Worktree git aparece com domínio padrão (não branch)

🔍 Diagnóstico:
```bash
cd /worktree/da/branch
cat .lerd.yaml | grep -A 3 worktree
git rev-parse --abbrev-ref HEAD     # qual branch é
```

🟢 Conserto: o auto-domínio por worktree requer `app_url` template no `.lerd.yaml` da branch principal:
```yaml
env_overrides:
  APP_URL: "{{scheme}}://{{branch}}.meusite.localhost"
```
Depois `lerd link` na worktree pega o template.

### 🔴 `.env` ficou com valores estranhos após `lerd init`

🔍 Diagnóstico:
```bash
ls -la .env.before_lerd             # backup automático
diff .env.before_lerd .env | head -30
```

🟢 Conserto: o `lerd env:restore` traz o backup de volta:
```bash
lerd env:restore                    # restaura .env.before_lerd
```

### 🔴 Após `lerd link`, comandos do framework não aparecem no dashboard

🔍 Diagnóstico:
```bash
cd /projeto
lerd framework
# Mostra framework detectado + comandos disponíveis
```

🟢 Conserto: o fork remove `migrate:fresh` e `doctrine:fixtures:load` dos defaults (perigosos demais pra one-click). Pra trazê-los de volta apenas neste projeto, no `.lerd.yaml`:
```yaml
commands:
  - name: migrate:fresh
    label: "Drop and re-migrate"
    command: "php artisan migrate:fresh --seed --force"
    confirm: true
    output: silent
```

## 💡 Dicas

- `lerd domain add foo` adiciona um domínio extra ao site atual (`foo.localhost` além de `meusite.localhost`).
- `lerd park ~/projetos` faz com que todo subdiretório em `~/projetos/` seja servido como site automaticamente.
- `sites.yaml` é per-machine; `.lerd.yaml` é per-project (commit no git). Não commit `sites.yaml`.
- Pra clonar `.lerd.yaml` em outra máquina e ativar tudo: `cd /projeto && lerd init` (detecta o existente e aplica).
