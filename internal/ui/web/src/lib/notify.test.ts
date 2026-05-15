import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { get } from 'svelte/store';

// Mock dashboard store so tests don't pull in services/site fetching.
const openOverlay = vi.fn();
vi.mock('$stores/dashboard', () => ({
  openOverlayUrl: (url: string) => openOverlay(url)
}));

// MockNotification stands in for the global Notification constructor.
class MockNotification {
  static permission: NotificationPermission = 'granted';
  static instances: MockNotification[] = [];
  static requestPermission = vi.fn(async () => MockNotification.permission);
  title: string;
  body?: string;
  tag?: string;
  constructor(title: string, opts?: NotificationOptions) {
    this.title = title;
    this.body = opts?.body;
    this.tag = opts?.tag;
    MockNotification.instances.push(this);
  }
  close() {
    /* noop */
  }
  onclick: (() => void) | null = null;
}

const swShows: Array<{ title: string; opts?: NotificationOptions }> = [];

function installSWMock() {
  const reg = {
    showNotification: vi.fn(async (title: string, opts?: NotificationOptions) => {
      swShows.push({ title, opts });
    })
  };
  Object.defineProperty(globalThis.navigator, 'serviceWorker', {
    configurable: true,
    value: {
      ready: Promise.resolve(reg),
      addEventListener: vi.fn()
    }
  });
}
function removeSWMock() {
  delete (globalThis.navigator as unknown as { serviceWorker?: unknown }).serviceWorker;
}

interface Notification {
  kind: string;
  title?: string;
  title_key?: string;
  body?: string;
  body_key?: string;
  params?: Record<string, string>;
  tag?: string;
  url?: string;
  data?: Record<string, string>;
}

describe('notify dispatcher', () => {
  beforeEach(() => {
    MockNotification.instances = [];
    MockNotification.permission = 'granted';
    swShows.length = 0;
    openOverlay.mockClear();
    // @ts-expect-error test double
    globalThis.Notification = MockNotification;
    localStorage.clear();
    installSWMock();
    vi.resetModules();
  });

  afterEach(() => {
    // @ts-expect-error reset
    delete globalThis.Notification;
    removeSWMock();
  });

  it('fires a notification via SW.showNotification when a WS notification arrives', async () => {
    const { initNotify } = await import('./notify');
    const { wsMessage } = await import('./ws');

    initNotify();
    const evt: Notification = {
      kind: 'mail',
      title: 'New email: Welcome',
      body: 'From: alice@x.com',
      tag: 'lerd-mail-abc',
      url: '#service/mailpit/view/abc',
      data: { id: 'abc' }
    };
    wsMessage.set({ type: 'notification', notification: evt });
    await Promise.resolve();
    await Promise.resolve();

    expect(swShows).toHaveLength(1);
    expect(swShows[0].title).toBe('New email: Welcome');
    expect(swShows[0].opts?.body).toBe('From: alice@x.com');
    expect(swShows[0].opts?.tag).toBe('lerd-mail-abc');
    expect((swShows[0].opts?.data as { kind?: string })?.kind).toBe('mail');
  });

  it('suppresses notifications for kinds the user has disabled', async () => {
    const { initNotify, setNotifyPref } = await import('./notify');
    const { wsMessage } = await import('./ws');

    initNotify();
    setNotifyPref('mail', false);

    wsMessage.set({
      type: 'notification',
      notification: { kind: 'mail', title: 'should not fire' }
    });
    await Promise.resolve();
    await Promise.resolve();

    expect(swShows).toHaveLength(0);
  });

  it('suppresses notifications when the master toggle is off', async () => {
    const { initNotify, setNotifyMaster } = await import('./notify');
    const { wsMessage } = await import('./ws');

    initNotify();
    setNotifyMaster(false);

    wsMessage.set({
      type: 'notification',
      notification: { kind: 'mail', title: 'masked' }
    });
    await Promise.resolve();
    await Promise.resolve();

    expect(swShows).toHaveLength(0);
  });

  it('deduplicates back-to-back notifications with the same tag', async () => {
    const { initNotify } = await import('./notify');
    const { wsMessage } = await import('./ws');

    initNotify();
    const evt: Notification = { kind: 'mail', title: 'x', tag: 'lerd-mail-dup' };
    wsMessage.set({ type: 'notification', notification: evt });
    wsMessage.set({ type: 'notification', notification: { ...evt } });
    await Promise.resolve();
    await Promise.resolve();

    expect(swShows).toHaveLength(1);
  });

  it('persists and exposes preferences', async () => {
    const { setNotifyPref, setNotifyMaster, notifyPrefs } = await import('./notify');

    setNotifyPref('dump', true);
    setNotifyMaster(false);

    const cur = get(notifyPrefs);
    expect(cur.kinds.dump).toBe(true);
    expect(cur.enabled).toBe(false);

    const stored = localStorage.getItem('lerd:notify:prefs');
    expect(stored).toBeTruthy();
    const parsed = JSON.parse(stored!);
    expect(parsed.kinds.dump).toBe(true);
    expect(parsed.enabled).toBe(false);
  });

  it('default prefs enable mail/worker/op/update and disable dump', async () => {
    const { notifyPrefs } = await import('./notify');
    const cur = get(notifyPrefs);
    expect(cur.enabled).toBe(true);
    expect(cur.kinds.mail).toBe(true);
    expect(cur.kinds.worker_failed).toBe(true);
    expect(cur.kinds.op_done).toBe(true);
    expect(cur.kinds.update_available).toBe(true);
    expect(cur.kinds.dump).toBe(false);
  });
});

describe('forgetCurrentBrowser', () => {
  interface FakeSub {
    endpoint: string;
    unsubscribe: ReturnType<typeof vi.fn>;
  }
  let fakeSub: FakeSub | null = null;
  let subscribeMock: ReturnType<typeof vi.fn>;

  function installPushMock(endpoint: string | null) {
    fakeSub = endpoint
      ? { endpoint, unsubscribe: vi.fn(async () => true) }
      : null;
    subscribeMock = vi.fn(async () => ({
      endpoint: 'https://push.example/new',
      toJSON: () => ({
        endpoint: 'https://push.example/new',
        keys: { p256dh: 'p', auth: 'a' }
      })
    }));
    const reg = {
      showNotification: vi.fn(),
      pushManager: {
        getSubscription: vi.fn(async () => fakeSub),
        subscribe: subscribeMock
      }
    };
    Object.defineProperty(globalThis.navigator, 'serviceWorker', {
      configurable: true,
      value: { ready: Promise.resolve(reg), addEventListener: vi.fn() }
    });
    // PushManager presence is the gate ensurePushSubscription checks.
    (globalThis as unknown as { PushManager?: object }).PushManager = function PushManager() {};
  }

  beforeEach(() => {
    MockNotification.permission = 'granted';
    // @ts-expect-error test double
    globalThis.Notification = MockNotification;
    localStorage.clear();
    vi.resetModules();
  });

  afterEach(() => {
    // @ts-expect-error reset
    delete globalThis.Notification;
    delete (globalThis as unknown as { PushManager?: object }).PushManager;
    delete (globalThis.navigator as unknown as { serviceWorker?: unknown }).serviceWorker;
  });

  it('unsubscribes and sets the flag when endpoint matches the current sub', async () => {
    installPushMock('https://push.example/mine');
    const { forgetCurrentBrowser, autoSubscribeDisabled } = await import('./notify');

    const result = await forgetCurrentBrowser('https://push.example/mine');

    expect(result).toBe(true);
    expect(fakeSub?.unsubscribe).toHaveBeenCalledTimes(1);
    expect(localStorage.getItem('lerd:notify:auto-subscribe')).toBe('0');
    expect(get(autoSubscribeDisabled)).toBe(true);
  });

  it('is a no-op when endpoint does not match', async () => {
    installPushMock('https://push.example/mine');
    const { forgetCurrentBrowser, autoSubscribeDisabled } = await import('./notify');

    const result = await forgetCurrentBrowser('https://push.example/somebody-else');

    expect(result).toBe(false);
    expect(fakeSub?.unsubscribe).not.toHaveBeenCalled();
    expect(localStorage.getItem('lerd:notify:auto-subscribe')).toBeNull();
    expect(get(autoSubscribeDisabled)).toBe(false);
  });

  it('is a no-op when the browser has no subscription', async () => {
    installPushMock(null);
    const { forgetCurrentBrowser, autoSubscribeDisabled } = await import('./notify');

    const result = await forgetCurrentBrowser('https://push.example/anything');

    expect(result).toBe(false);
    expect(get(autoSubscribeDisabled)).toBe(false);
  });

  it('initNotify skips ensurePushSubscription when the flag is set', async () => {
    installPushMock('https://push.example/mine');
    localStorage.setItem('lerd:notify:auto-subscribe', '0');

    const reg = await navigator.serviceWorker!.ready;
    const getSub = (reg as unknown as { pushManager: { getSubscription: ReturnType<typeof vi.fn> } })
      .pushManager.getSubscription;

    const { initNotify } = await import('./notify');
    initNotify();
    await Promise.resolve();
    await Promise.resolve();

    expect(getSub).not.toHaveBeenCalled();
  });

  it('enableNotifications clears the flag and triggers a re-subscribe', async () => {
    installPushMock(null);
    localStorage.setItem('lerd:notify:auto-subscribe', '0');

    const { enableNotifications, autoSubscribeDisabled } = await import('./notify');
    // apiFetch will try to hit /api/push/vapid-public-key — stub fetch so
    // ensurePushSubscription's branch returns silently instead of throwing.
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(null, { status: 404 })
    );

    const res = await enableNotifications();
    await Promise.resolve();

    expect(res).toBe('granted');
    expect(localStorage.getItem('lerd:notify:auto-subscribe')).toBeNull();
    expect(get(autoSubscribeDisabled)).toBe(false);

    fetchSpy.mockRestore();
  });
});

describe('detectBrowserFamily', () => {
  it('classifies Chrome as chromium', async () => {
    const { detectBrowserFamily } = await import('./notify');
    expect(
      detectBrowserFamily(
        'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36'
      )
    ).toBe('chromium');
  });

  it('classifies Edge as chromium', async () => {
    const { detectBrowserFamily } = await import('./notify');
    expect(
      detectBrowserFamily(
        'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 Edg/130.0.0.0'
      )
    ).toBe('chromium');
  });

  it('classifies Opera as chromium', async () => {
    const { detectBrowserFamily } = await import('./notify');
    expect(
      detectBrowserFamily(
        'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 OPR/115.0.0.0'
      )
    ).toBe('chromium');
  });

  it('classifies Firefox as firefox', async () => {
    const { detectBrowserFamily } = await import('./notify');
    expect(
      detectBrowserFamily(
        'Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0'
      )
    ).toBe('firefox');
  });

  it('classifies Safari (without Chrome) as safari', async () => {
    const { detectBrowserFamily } = await import('./notify');
    expect(
      detectBrowserFamily(
        'Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15'
      )
    ).toBe('safari');
  });

  it('falls back to other for empty or unknown UAs', async () => {
    const { detectBrowserFamily } = await import('./notify');
    expect(detectBrowserFamily('')).toBe('other');
    expect(detectBrowserFamily('SomeCustomBot/1.0')).toBe('other');
  });
});
