# Workers (queue, horizon, schedule, reverb) — Debug

[← voltar pro índice](../DEBUG.md)

## Como funciona

Cada worker é uma systemd user unit que executa `podman exec lerd-php<X>-fpm <comando>` dentro do container FPM do site:

```
   lerd-<site>-queue.service      ─→ podman exec lerd-php85-fpm php artisan queue:work
   lerd-<site>-horizon.service    ─→ podman exec lerd-php85-fpm php artisan horizon
   lerd-<site>-schedule.service   ─→ podman exec lerd-php85-fpm php artisan schedule:work
   lerd-<site>-reverb.service     ─→ podman exec lerd-php85-fpm php artisan reverb:start
```

Cada unit tem `BindsTo=lerd-php<X>-fpm.service`: se o FPM cai, o worker cai junto. Se o FPM volta, o worker NÃO sobe automático (lerd faz isso explicitamente em `lerd php:rebuild`).

## Problemas comuns

### 🔴 `lerd queue:start` deixou o worker em `failed`

🔍 Diagnóstico:
```bash
systemctl --user status lerd-<site>-queue.service
journalctl --user -u lerd-<site>-queue.service -n 50 --no-pager
```

Causas comuns:
- `php artisan queue:work` falhou na inicialização (Redis/DB indisponível)
- O FPM target do `BindsTo` está parado
- Crash loop ultrapassou `StartLimitBurst`

🟢 Conserto:
```bash
systemctl --user reset-failed lerd-<site>-queue.service
systemctl --user start lerd-php85-fpm                 # garante o target
lerd queue:start                                      # restart limpo
```

### 🔴 Horizon mostra "Inactive" no dashboard mesmo com unit ativa

🔍 Diagnóstico:
```bash
systemctl --user is-active lerd-<site>-horizon
lerd php artisan horizon:status
```

🟢 Conserto: Horizon mantém seu próprio estado em Redis. Se Redis foi resetado:
```bash
lerd php artisan horizon:terminate
systemctl --user restart lerd-<site>-horizon
```

### 🔴 Worker reiniciando em loop, log com "container is not running"

⚠️ O `BindsTo` rapidamente reinicia o worker quando o FPM volta — mas se o FPM está num crash loop próprio, o worker também loopa.

🔍 Diagnóstico:
```bash
systemctl --user is-active lerd-php85-fpm
journalctl --user -u lerd-php85-fpm.service -n 30 --no-pager
```

🟢 Conserto: corrija o FPM primeiro (veja [php-fpm.md](php-fpm.md)). O worker vai voltar sozinho via `BindsTo`.

### 🔴 Schedule não dispara cron jobs

🔍 Diagnóstico:
```bash
systemctl --user is-active lerd-<site>-schedule
lerd php artisan schedule:list
# Aparece a lista? Se não, verifique app/Console/Kernel.php / routes/console.php
```

🟢 Conserto: schedule:work checa a cada minuto. Se o problema é horário (timezone), confira:
```bash
podman exec lerd-php85-fpm date
podman exec lerd-php85-fpm php -r 'echo date_default_timezone_get();'
```
Se diferente do host, ajuste em `.env`: `APP_TIMEZONE=America/Sao_Paulo`.

### 🔴 Reverb (WebSocket) — conexão estabelecida mas não recebe broadcasts

🔍 Diagnóstico:
```bash
podman logs lerd-php85-fpm 2>&1 | grep -i reverb | tail -20
# Confira se BROADCAST_CONNECTION=reverb e se a porta REVERB_SERVER_PORT está livre
```

🟢 Conserto:
```bash
lerd reverb:stop && lerd reverb:start
# Testar: artisan reverb:debug
```

### 🔴 Stripe webhook handler não roda mesmo com unit ativa

🔍 Diagnóstico:
```bash
podman ps | grep stripe-mock                          # serviço stripe-mock subido?
cat .env | grep STRIPE_SECRET                         # secret presente?
journalctl --user -u lerd-<site>-stripe.service -n 20
```

🟢 Conserto: o worker stripe só roda se `STRIPE_SECRET` está no `.env`. Caso contrário lerd não cria a unit.

### 🔴 `lerd worker heal` não consegue resuscitar todos

🔍 Diagnóstico:
```bash
lerd worker heal --dry-run                            # mostra o que tentaria fazer
```

🟢 Conserto:
```bash
lerd worker heal                                      # roda restart em cascata respeitando deps
# Se persistir, restart manual em ordem:
systemctl --user restart lerd-php85-fpm
systemctl --user restart lerd-<site>-queue lerd-<site>-horizon lerd-<site>-schedule lerd-<site>-reverb
```

## 💡 Dicas

- `lerd horizon` (sem args) mostra status combinado.
- O dashboard tem um banner global mostrando workers em estado `failed` — sempre dá pra clicar pra ver detalhe.
- Pra desabilitar um worker num projeto, edite `.lerd.yaml`:
  ```yaml
  workers: [queue, schedule]    # remove "horizon" se não quer
  ```
- Workers de worktree (branches git) recebem domínio próprio (`branch.site.localhost`) e workers próprios.
