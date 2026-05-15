# Notifications

The dashboard can pop OS-level notifications for events you'd otherwise have to keep an eye on a tab to catch: a captured email, a worker that just crashed, a long-running service operation that finished, a new image tag available for a service, or a `ray()`/`dump()` arriving from a site you're debugging. Notifications fire even when the dashboard tab is minimised, in the background, or fully closed — they're delivered via Web Push, which wakes the registered service worker through your browser vendor's push infrastructure (FCM for Chrome/Brave/Edge, Mozilla autopush for Firefox).

Lerd ships notifications **off by default**. The first time you open the dashboard a small banner offers to enable them; clicking *Enable* prompts your browser for permission once. Granting it is sticky — the dashboard re-uses the permission across sessions and re-registers the push subscription on every page load so the server's subscription list stays in sync after browser resets or sub expiry.

## Notification categories

| Kind | Fires when | Default | Urgency |
| --- | --- | --- | --- |
| `mail` | Mailpit captures an outgoing email | on | normal |
| `worker_failed` | A queue / horizon / reverb / schedule / stripe worker enters the `failed` state | on | high |
| `op_done` / `op_failed` | A streaming service operation (install, migrate, reinstall, update, rollback) finishes | on | normal / high |
| `update_available` | The registry has a newer image tag for an installed service | on | low |
| `dump` | A `ray()` / `dump()` / var-dump packet arrives | **off** | low |

Each category can be toggled individually under **System → Notifications**, along with a master switch that turns every category off in one click. Preferences are stored client-side in `localStorage` and mirrored to the server via the push subscription — closed-PWA push respects the toggles even when the dashboard isn't running.

Clicking a notification focuses the dashboard (or launches the PWA if closed) and deep-links to the relevant view: the captured email in the Mailpit overlay, the failing worker's site detail, the finished service's tile, the Dumps tab.

## How it works

Two delivery paths run in parallel:

1. **WebSocket fan-out** (open tabs). Every notification rides the existing `/api/ws` channel as a `notification` frame. Open dashboard tabs route it through `lib/notify.ts`, which resolves the i18n key with Paraglide and calls `registration.showNotification(...)` so the toast lands in the OS notification center with a persistent click target.
2. **Web Push** (closed tabs / installed PWA). When permission is granted, the page subscribes via `pushManager.subscribe()` using the install's VAPID public key. The server stores the subscription endpoint plus the user's per-category preferences and, on every notification, sends an encrypted Web Push (RFC 8291) to each allow-listed subscription. The browser wakes the service worker, the SW shows the notification with the same payload shape it received over the WS.

Both paths receive the same JSON payload: `{kind, title, title_key, body, body_key, params, tag, url, data, icon}`. The SW uses `title`/`body` directly (no DOM, no Paraglide); the page uses `title_key`/`body_key` with `params` for proper localisation.

## Localisation

Notification copy is translatable. Every category has paraglide keys under `notify_*` in `internal/ui/web/messages/<locale>.json` — `notify_mail_title`, `notify_worker_failed_body`, `notify_op_done_title`, etc. The page side resolves them through Paraglide using the user's selected locale. The server-side English fallback is always sent in `title`/`body` so the SW (which has no DOM and no Paraglide bundle) can still render correctly when the tab is closed.

To add a new locale, drop a new file under `internal/ui/web/messages/` and copy the `notify_*` keys.

## Server state

- **VAPID key pair**: `~/.local/share/lerd/vapid-private.key` (mode 0600) + `vapid-public.key`. Generated lazily on first call. Re-using the same pair keeps existing subscriptions valid across lerd-ui restarts.
- **Subscriptions**: `~/.local/share/lerd/push-subscriptions.json`. One entry per browser. Each entry records the push endpoint, encryption keys, the User-Agent that subscribed, the timestamp, the master `enabled` flag, and the per-kind `enabled_kinds` allow-list.

Subscriptions that the push service retires (HTTP 410 Gone, 404 Not Found) are pruned automatically on the next send attempt — no manual cleanup needed.

## Settings panel

**System → Notifications** is the canonical control surface:

- *Master switch* — overrides every per-category toggle.
- *Per-category toggles* — one row per kind with a short description.
- *Send a test notification* — fires a no-op `test` push so you can verify the full pipeline (WS + push + SW) end-to-end. The `test` kind always passes the category filter so you can test even with everything muted.
- *Subscribed devices* — lists every browser that has subscribed (truncated UA + added-at timestamp). Click *Forget* to revoke a device's subscription server-side without touching the browser-side permission.

## Endpoints

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/push/vapid-public-key` | Returns the install's VAPID public key for `pushManager.subscribe`. |
| `POST` | `/api/push/subscribe` | Stores a new subscription or updates an existing one's preferences. |
| `POST` | `/api/push/unsubscribe` | Removes a subscription by endpoint. |
| `GET` | `/api/push/devices` | Lists subscribed devices (sanitised; never returns the p256dh/auth secrets). |
| `POST` | `/api/push/test` | Dispatches a hard-coded test notification through the central notifier. |

All endpoints sit behind the standard `withRemoteControlGate` (loopback-only by default). The mailpit webhook at `/api/webhooks/mailpit` has its own explicit bypass — the threat surface is bounded (a LAN attacker could spoof fake mail notifications) and lerd is a local-dev tool.

## Disabling notifications

Three layers; each is enough on its own:

- Toggle off in **System → Notifications** (per-category or master).
- Reset the browser's notification permission for the dashboard origin (Brave / Chrome / Firefox → site settings).
- `rm ~/.local/share/lerd/push-subscriptions.json` — server-side wipe; the browser will silently re-subscribe on the next page load if permission is still granted, so combine with the first two options if you want a permanent uninstall.

## Browser support

- **Chromium-based** (Brave, Chrome, Edge): full support, including Web Push to a closed PWA. `lerd.localhost` is treated as a secure context.
- **Firefox 84+**: full support; same secure-context treatment.
- **Safari**: Notification API only works when opened at `http://localhost:7073` directly. Safari doesn't grant secure-context status to `.localhost` subdomains, so `lerd.localhost` is silently non-functional.

For installed PWAs, force a service-worker update if notifications stop popping after a lerd upgrade: open DevTools → Application → Service Workers and click *Update*, or run `(await navigator.serviceWorker.getRegistration()).unregister()` in the console and reload.
