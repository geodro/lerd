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
