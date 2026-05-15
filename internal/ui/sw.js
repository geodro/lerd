const VERSION = '{{LERD_VERSION}}';
const CACHE = 'lerd-shell-' + VERSION;

const SHELL = [
  '/offline.html',
  '/manifest.webmanifest',
  '/icons/icon-192.png',
  '/icons/icon-512.png',
  '/icons/icon.svg',
];

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE)
      .then((cache) => cache.addAll(SHELL))
      .then(() => self.skipWaiting())
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) => Promise.all(
      keys.filter((k) => k.startsWith('lerd-shell-') && k !== CACHE).map((k) => caches.delete(k))
    )).then(() => self.clients.claim())
  );
});

// push fires when the browser receives a Web Push from lerd-ui. Payload is
// the kind-agnostic push.Notification JSON (kind, title, body, tag, url,
// data, params, title_key, body_key). We show whatever the server sent
// using title/body directly — the SW has no DOM and no Paraglide, so the
// English fallback strings are what the user sees here. Page-context
// dispatch (see notify.ts) uses the i18n keys when the dashboard is open.
self.addEventListener('push', (event) => {
  let evt;
  try {
    evt = event.data ? event.data.json() : null;
  } catch (_) {
    evt = null;
  }
  if (!evt || !evt.kind) return;
  const title = (evt.title || '').trim() || '(lerd)';
  const body = (evt.body || '').trim();
  event.waitUntil(self.registration.showNotification(title, {
    body,
    tag: evt.tag || ('lerd-' + evt.kind),
    icon: evt.icon || '/icons/icon-192.png',
    data: {
      kind: evt.kind,
      url: evt.url || '',
      // Forward any extras the producer attached so notificationclick can
      // route or analytics can read them.
      ...(evt.data || {})
    }
  }));
});

// notificationclick is kind-agnostic: every notification carries a `url`
// hash-route in its data, and we either post that to an existing client
// (so the dashboard navigates without a full reload) or open a fresh
// window pointed at the hash. The page-side hashchange listener resolves
// services + deep-link parameters from there.
self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const data = event.notification.data || {};
  const url = data.url || '';
  event.waitUntil((async () => {
    const all = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
    for (const c of all) {
      if (new URL(c.url).origin !== self.location.origin) continue;
      try {
        await c.focus();
      } catch (_) {
        /* focus may be disallowed; postMessage still works */
      }
      c.postMessage({ kind: 'lerd-open', url });
      return;
    }
    if (self.clients.openWindow) {
      await self.clients.openWindow('/' + url);
    }
  })());
});

self.addEventListener('fetch', (event) => {
  const req = event.request;
  if (req.method !== 'GET') return;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;
  if (url.pathname.startsWith('/api/')) return;

  if (req.mode === 'navigate') {
    event.respondWith((async () => {
      try {
        return await fetch(req);
      } catch (_) {
        const fallback = await caches.match('/offline.html');
        return fallback || new Response('lerd-ui unreachable', { status: 503, headers: { 'Content-Type': 'text/plain' } });
      }
    })());
    return;
  }

  event.respondWith((async () => {
    const cached = await caches.match(req);
    if (cached) return cached;
    try {
      const res = await fetch(req);
      if (res && res.ok && res.type === 'basic') {
        const copy = res.clone();
        caches.open(CACHE).then((cache) => cache.put(req, copy)).catch(() => {});
      }
      return res;
    } catch (err) {
      // The browser aborts in-flight requests when the user navigates away or
      // a preload is cancelled; returning 503 here dirties the devtools console
      // with a scary red row. If we have a cached copy, reuse it; otherwise
      // surface a transparent "aborted" response with no status so DevTools
      // marks it as (failed) rather than 503.
      if (err && (err.name === 'AbortError' || /aborted|cancel/i.test(String(err.message)))) {
        return Response.error();
      }
      return new Response('', { status: 503 });
    }
  })());
});
