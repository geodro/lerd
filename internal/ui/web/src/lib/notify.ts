import { writable, get } from 'svelte/store';
import { wsMessage, type NotificationEvent } from './ws';
import { apiFetch } from './api';
import { m } from '../paraglide/messages.js';

const PREFS_KEY = 'lerd:notify:prefs';
const DISMISS_KEY = 'lerd:notify:dismissed';
// AUTO_SUB_KEY records "user has forgotten this browser; don't silently
// re-subscribe on next mount". Set to "0" by forgetCurrentBrowser, cleared
// by enableNotifications. Without it, ensurePushSubscription would re-post
// the same endpoint right after Forget undeleted the row from the server.
const AUTO_SUB_KEY = 'lerd:notify:auto-subscribe';

// NotifyKind is the canonical set of notification categories the user can
// toggle. The list lives client-side because the page-context dispatcher is
// the only filter; the backend forwards every kind and trusts each
// subscription's stored EnabledKinds to gate Web Push delivery.
export type NotifyKind =
  | 'mail'
  | 'worker_failed'
  | 'op_done'
  | 'update_available'
  | 'dump';

export const ALL_KINDS: NotifyKind[] = [
  'mail',
  'worker_failed',
  'op_done',
  'update_available',
  'dump'
];

export interface NotifyPrefs {
  enabled: boolean;
  kinds: Record<NotifyKind, boolean>;
}

const DEFAULTS: NotifyPrefs = {
  enabled: true,
  kinds: {
    mail: true,
    worker_failed: true,
    op_done: true,
    update_available: true,
    // dump is opt-in: many dev sessions emit hundreds of ray() calls and
    // the user almost always wants to silence them by default.
    dump: false
  }
};

function loadPrefs(): NotifyPrefs {
  if (typeof localStorage === 'undefined') return clonePrefs(DEFAULTS);
  const raw = localStorage.getItem(PREFS_KEY);
  if (!raw) return clonePrefs(DEFAULTS);
  try {
    const p = JSON.parse(raw) as Partial<NotifyPrefs>;
    return {
      enabled: p.enabled ?? DEFAULTS.enabled,
      kinds: { ...DEFAULTS.kinds, ...(p.kinds ?? {}) } as Record<NotifyKind, boolean>
    };
  } catch {
    return clonePrefs(DEFAULTS);
  }
}

function clonePrefs(p: NotifyPrefs): NotifyPrefs {
  return { enabled: p.enabled, kinds: { ...p.kinds } };
}

function savePrefs(p: NotifyPrefs) {
  if (typeof localStorage === 'undefined') return;
  localStorage.setItem(PREFS_KEY, JSON.stringify(p));
}

export const notifyPrefs = writable<NotifyPrefs>(loadPrefs());
export const permissionState = writable<NotificationPermission | 'unsupported'>(
  typeof Notification === 'undefined' ? 'unsupported' : Notification.permission
);
export const dismissed = writable<boolean>(
  typeof localStorage !== 'undefined' && localStorage.getItem(DISMISS_KEY) === '1'
);
export const autoSubscribeDisabled = writable<boolean>(isAutoSubscribeDisabled());

function isAutoSubscribeDisabled(): boolean {
  if (typeof localStorage === 'undefined') return false;
  return localStorage.getItem(AUTO_SUB_KEY) === '0';
}

function setAutoSubscribeDisabled(off: boolean) {
  if (typeof localStorage !== 'undefined') {
    if (off) localStorage.setItem(AUTO_SUB_KEY, '0');
    else localStorage.removeItem(AUTO_SUB_KEY);
  }
  autoSubscribeDisabled.set(off);
}

export function setNotifyPref(kind: NotifyKind, on: boolean) {
  notifyPrefs.update((p) => {
    const next = { enabled: p.enabled, kinds: { ...p.kinds, [kind]: on } };
    savePrefs(next);
    void syncSubscriptionPrefs(next);
    return next;
  });
}

export function setNotifyMaster(on: boolean) {
  notifyPrefs.update((p) => {
    const next = { enabled: on, kinds: { ...p.kinds } };
    savePrefs(next);
    void syncSubscriptionPrefs(next);
    return next;
  });
}

export function dismissNotifyBanner() {
  if (typeof localStorage !== 'undefined') localStorage.setItem(DISMISS_KEY, '1');
  dismissed.set(true);
}

// localizedTitle / localizedBody resolve a Paraglide key with params,
// falling back to the raw English string from the payload when the key is
// missing or Paraglide hasn't compiled a message for it. This is how the
// page achieves localisation while the SW (no DOM, no Paraglide) keeps
// showing the English fallback.
function localize(
  key: string | undefined,
  fallback: string | undefined,
  params: Record<string, string> | undefined
): string {
  if (key) {
    const fn = (m as unknown as Record<string, (p?: Record<string, string>) => string>)[key];
    if (typeof fn === 'function') {
      try {
        return fn(params ?? {});
      } catch {
        /* fall through to fallback */
      }
    }
  }
  return fallback ?? '';
}

let lastTag: string | null = null;

async function fireNotification(evt: NotificationEvent) {
  if (typeof Notification === 'undefined') return;
  if (Notification.permission !== 'granted') return;
  const prefs = get(notifyPrefs);
  if (!prefs.enabled) return;
  if (prefs.kinds[evt.kind as NotifyKind] === false) return;
  if (evt.tag && evt.tag === lastTag) return;
  if (evt.tag) lastTag = evt.tag;

  const title = localize(evt.title_key, evt.title, evt.params) || '(notification)';
  const body = localize(evt.body_key, evt.body, evt.params) || '';
  const opts: NotificationOptions = {
    body,
    tag: evt.tag,
    icon: evt.icon ?? '/icons/icon-192.png',
    data: { kind: evt.kind, url: evt.url ?? '', ...(evt.data ?? {}) }
  };

  if ('serviceWorker' in navigator) {
    try {
      const reg = await navigator.serviceWorker.ready;
      await reg.showNotification(title, opts);
      return;
    } catch {
      /* fall through to page-level Notification */
    }
  }
  new Notification(title, opts);
}

let initialized = false;

export function initNotify() {
  if (initialized) return;
  initialized = true;
  wsMessage.subscribe((msg) => {
    if (!msg?.notification) return;
    void fireNotification(msg.notification);
  });
  if ('serviceWorker' in navigator) {
    navigator.serviceWorker.addEventListener('message', (e: MessageEvent) => {
      const data = e.data as { kind?: string; url?: string } | undefined;
      if (data?.kind === 'lerd-open' && data.url) {
        openOverlayUrl(data.url);
      }
    });
  }
  // Re-register the push subscription on every mount when permission is
  // already granted so the server's subscription list stays in sync after
  // a browser reset, sub expiry, or pref change made while offline.
  // Skipped when the user has clicked Forget on this browser — otherwise
  // the row they just deleted would reappear on the next page load.
  if (
    typeof Notification !== 'undefined' &&
    Notification.permission === 'granted' &&
    !isAutoSubscribeDisabled()
  ) {
    void ensurePushSubscription();
  }
}

export function shouldShowOptIn(): boolean {
  if (typeof Notification === 'undefined') return false;
  if (Notification.permission !== 'default') return false;
  return !get(dismissed);
}

// BrowserFamily is the smallest classification we need for picking the right
// "how to unblock notifications" copy. Edge / Brave / Opera all collapse into
// chromium because their site-permissions UI is the lock/icon flow.
export type BrowserFamily = 'chromium' | 'firefox' | 'safari' | 'other';

export function detectBrowserFamily(ua: string): BrowserFamily {
  if (!ua) return 'other';
  if (/Firefox\//.test(ua)) return 'firefox';
  if (/Edg\/|OPR\/|Chrome\//.test(ua)) return 'chromium';
  if (/Safari\//.test(ua)) return 'safari';
  return 'other';
}

export async function enableNotifications(): Promise<NotificationPermission | 'unsupported'> {
  if (typeof Notification === 'undefined') return 'unsupported';
  let result: NotificationPermission;
  if (Notification.permission === 'granted' || Notification.permission === 'denied') {
    result = Notification.permission;
  } else {
    result = await Notification.requestPermission();
  }
  permissionState.set(result);
  if (result === 'granted') {
    // Explicit user opt-in clears any prior Forget — without this the
    // user could click "Subscribe this browser" forever and nothing would
    // happen because initNotify already skipped ensurePushSubscription.
    setAutoSubscribeDisabled(false);
    void ensurePushSubscription();
  }
  return result;
}

// forgetCurrentBrowser is called by the settings panel's Forget button when
// the removed device matches the current browser. It revokes the live
// PushSubscription so the browser's push service stops hitting our endpoint,
// then sets the auto-subscribe-disabled flag so initNotify on the next
// mount doesn't silently re-register the same browser.
export async function forgetCurrentBrowser(endpoint: string): Promise<boolean> {
  if (!('serviceWorker' in navigator)) return false;
  try {
    const reg = await navigator.serviceWorker.ready;
    const sub = await reg.pushManager.getSubscription();
    if (!sub || sub.endpoint !== endpoint) return false;
    await sub.unsubscribe();
    setAutoSubscribeDisabled(true);
    return true;
  } catch (err) {
    console.warn('[lerd] forget current browser failed:', err);
    return false;
  }
}

// urlBase64ToArrayBuffer decodes a base64url VAPID public key into the
// ArrayBuffer pushManager.subscribe wants for applicationServerKey.
function urlBase64ToArrayBuffer(b64: string): ArrayBuffer {
  const padding = '='.repeat((4 - (b64.length % 4)) % 4);
  const base64 = (b64 + padding).replace(/-/g, '+').replace(/_/g, '/');
  const raw = atob(base64);
  const out = new ArrayBuffer(raw.length);
  const view = new Uint8Array(out);
  for (let i = 0; i < raw.length; i++) view[i] = raw.charCodeAt(i);
  return out;
}

async function ensurePushSubscription(): Promise<void> {
  if (!('serviceWorker' in navigator) || !('PushManager' in window)) return;
  try {
    const reg = await navigator.serviceWorker.ready;
    let sub = await reg.pushManager.getSubscription();
    if (!sub) {
      const r = await apiFetch('/api/push/vapid-public-key');
      if (!r.ok) return;
      const { public_key: pubKey } = (await r.json()) as { public_key: string };
      if (!pubKey) return;
      sub = await reg.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: urlBase64ToArrayBuffer(pubKey)
      });
    }
    await postSubscription(sub);
  } catch (err) {
    console.warn('[lerd] push subscribe failed:', err);
  }
}

async function postSubscription(sub: PushSubscription) {
  const prefs = get(notifyPrefs);
  await apiFetch('/api/push/subscribe', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      ...sub.toJSON(),
      enabled: prefs.enabled,
      enabled_kinds: Object.entries(prefs.kinds)
        .filter(([, on]) => on)
        .map(([k]) => k)
    })
  });
}

// syncSubscriptionPrefs pushes the latest prefs to the backend so closed-PWA
// Web Push respects them. Best-effort; ignored if no subscription exists.
async function syncSubscriptionPrefs(_p: NotifyPrefs) {
  if (!('serviceWorker' in navigator)) return;
  try {
    const reg = await navigator.serviceWorker.ready;
    const sub = await reg.pushManager.getSubscription();
    if (!sub) return;
    await postSubscription(sub);
  } catch {
    /* non-fatal; will re-sync on next page mount */
  }
}

// openOverlayUrl is the SW-message click handler — opens the deep-linked
// overlay when the user clicks an OS notification while the dashboard tab
// is open. Imported lazily so notify.ts has no compile-time dependency on
// the dashboard store (and so the test file can mock it).
function openOverlayUrl(url: string) {
  // Setting the hash kicks initDashboardRoute / hashchange listener which
  // resolves the right service + extraPath; see stores/dashboard.ts.
  if (typeof location !== 'undefined' && url.startsWith('#')) {
    location.hash = url.slice(1);
  }
}

export function _resetNotifyForTest() {
  initialized = false;
  lastTag = null;
  notifyPrefs.set(clonePrefs(DEFAULTS));
  dismissed.set(false);
  autoSubscribeDisabled.set(false);
  if (typeof Notification !== 'undefined') {
    permissionState.set(Notification.permission);
  }
}
