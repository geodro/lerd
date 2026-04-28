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
