# Stripe

The webhook listener works for any project type, not just Laravel. It runs the Stripe CLI in a container and forwards events to your app, so a NestJS or plain Node site reached through the host proxy is supported the same way a Laravel Cashier site is. The secret is auto-detected from common env keys and the forward route is configurable per project.

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
lerd stripe:listen --path /webhooks/stripe # custom webhook path (persisted to .lerd.yaml)
lerd stripe:listen --api-key sk_test_...   # override key
lerd stripe:listen --secret-env-key STRIPE_SECRET_KEY # pin the .env key (persisted)
```

The target URL is auto-detected from the registered site in the current directory. Run `lerd link` first if the project is not yet registered.

### Secret detection

Lerd resolves the Stripe secret from the project's `.env` automatically, probing these keys in order until one is set:

1. `STRIPE_SECRET` — Laravel / Cashier
2. `STRIPE_SECRET_KEY` — the common Stripe Node / NestJS convention
3. `STRIPE_API_KEY` — the generic SDK name

No flags are required if any of these is present. To force a specific key (for example a project that stores it under a non-standard name), pin it with `--secret-env-key` or the `stripe.secret_env_key` field in `.lerd.yaml`.

### Configuring the route without starting

`lerd stripe:config` shows or sets the webhook path and secret env key without touching the listener:

```bash
lerd stripe:config                              # show current route and resolved secret key
lerd stripe:config --path /webhooks/stripe      # set the forward route
lerd stripe:config --secret-env-key STRIPE_API_KEY
```

Both `stripe:config` and `stripe:listen` write the same optional `stripe:` block to the project's `.lerd.yaml`, so the route survives reinstalls and is shared by the CLI, the MCP server, and the web UI:

```yaml
# .lerd.yaml
stripe:
  path: /webhooks/stripe      # optional, defaults to /stripe/webhook
  secret_env_key: STRIPE_API_KEY  # optional, defaults to auto-detection
```

Both fields are optional. Routes are normalised to a leading slash and rejected if they contain whitespace, so the generated systemd unit stays well formed. Changing the route while the listener is running re-forwards it to the new path automatically.

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

Logs are also available live in the **web UI**, see [Web UI](#web-ui) below.

### Options

These flags apply to `stripe:listen`; `stripe:config` accepts `--path` and `--secret-env-key` only.

| Flag | Default | Description |
|---|---|---|
| `--api-key` | resolved from `.env` | Stripe secret key (`sk_test_…` or `sk_live_…`) |
| `--path` | `/stripe/webhook` | Webhook route path on your app (persisted to `.lerd.yaml`) |
| `--secret-env-key` | auto-detected | Which `.env` key holds the Stripe secret (persisted to `.lerd.yaml`) |

### Web UI

When a Stripe secret is detected in a site's `.env` (under any of the recognised keys), a **Stripe** toggle appears in the site detail panel alongside HTTPS and Queue. Toggling it starts or stops the listener. A gear next to the toggle opens a small modal for setting the webhook route, mirroring the Horizon control. While running:

- A violet dot appears next to the site in the sidebar.
- A **Stripe** log tab opens automatically beside PHP-FPM and Queue.
- The listener also appears in the **Services** tab with a `stripe` badge.
