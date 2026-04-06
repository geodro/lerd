# Stripe / Laravel Cashier

## stripe-mock

A local Stripe API mock for feature tests that exercise Cashier without hitting the real API and without needing a Stripe account.

```yaml
# ~/.config/lerd/services/stripe-mock.yaml
name: stripe-mock
image: docker.io/stripemock/stripe-mock:latest
description: "Local Stripe API mock for Cashier testing"
ports:
  - 12111:12111
```

```bash
lerd service add ~/.config/lerd/services/stripe-mock.yaml
lerd service start stripe-mock
```

Point the Stripe PHP SDK at the mock in your `AppServiceProvider` or test bootstrap:

```php
\Stripe\Stripe::$apiBase = 'http://lerd-stripe-mock:12111';
```

---

## stripe:listen

Forwards live or test webhook events from Stripe to your local app. Lerd runs the Stripe CLI in a container as a background **systemd user service**, so it persists across terminal sessions and restarts automatically on failure.

### Starting the listener

```bash
cd ~/Lerd/myapp
lerd stripe:listen                         # forwards to https://myapp.test/stripe/webhook
lerd stripe:listen --path /webhooks/stripe # custom webhook path
lerd stripe:listen --api-key sk_test_...   # override key
```

Lerd reads `STRIPE_SECRET` automatically from the project's `.env` file. No flags are required if the key is set there.

The target URL is auto-detected from the registered site in the current directory. Run `lerd link` first if the project is not yet registered.

### Stopping the listener

```bash
lerd stripe:listen stop
```

Starting and stopping the listener updates the `workers` list in `.lerd.yaml` (when the file exists), so the stripe listener is restored automatically after a reinstall when you run `lerd start`.

### HTTPS

If you run `lerd secure` or `lerd unsecure` while the listener is active, Lerd automatically restarts it so `--forward-to` stays in sync with the site's current scheme. No manual restart needed.

### Logs

```bash
journalctl --user -u lerd-stripe-myapp -f
```

Logs are also available live in the **web UI** — see [Web UI](#web-ui) below.

### Options

| Flag | Default | Description |
|---|---|---|
| `--api-key` | `$STRIPE_SECRET` / `.env` | Stripe secret key (`sk_test_…` or `sk_live_…`) |
| `--path` | `/stripe/webhook` | Webhook route path on your app |

### Web UI

When `STRIPE_SECRET` is present in a site's `.env`, a **Stripe** toggle appears in the site detail panel alongside HTTPS and Queue. Toggling it starts or stops the listener. While running:

- A violet dot appears next to the site in the sidebar.
- A **Stripe** log tab opens automatically beside PHP-FPM and Queue.
- The listener also appears in the **Services** tab with a `stripe` badge.
