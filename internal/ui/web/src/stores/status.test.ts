import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { get } from 'svelte/store';

describe('status store', () => {
  const realFetch = globalThis.fetch;

  beforeEach(() => {
    vi.resetModules();
  });

  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  it('loads status from /api/status', async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response(
        JSON.stringify({
          dns: { ok: true, tld: 'test' },
          nginx: { running: true },
          php_fpms: [{ version: '8.5', running: true, xdebug_enabled: false }],
          php_default: '8.5',
          node_default: '22',
          node_managed_by_lerd: true,
          watcher_running: true
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      )
    ) as unknown as typeof fetch;

    const { status, loadStatus, statusLoaded } = await import('./status');
    await loadStatus();
    expect(get(statusLoaded)).toBe(true);
    expect(get(status).dns.ok).toBe(true);
    expect(get(status).php_default).toBe('8.5');
  });

  it('lerdStatusColor is gray before load', async () => {
    const { lerdStatusColor } = await import('./status');
    expect(get(lerdStatusColor)).toBe('gray');
  });

  it('lerdStatusColor is red when core is broken', async () => {
    const { status, statusLoaded, lerdStatusColor } = await import('./status');
    statusLoaded.set(true);
    status.update((s) => ({
      ...s,
      dns: { ok: false, tld: 'test' },
      nginx: { running: true },
      watcher_running: true
    }));
    expect(get(lerdStatusColor)).toBe('red');
  });

  it('lerdStatusColor is yellow when healthy with update', async () => {
    const { status, statusLoaded, lerdStatusColor } = await import('./status');
    const { version } = await import('./version');
    statusLoaded.set(true);
    status.update((s) => ({
      ...s,
      dns: { ok: true, tld: 'test' },
      nginx: { running: true },
      watcher_running: true
    }));
    version.update((v) => ({ ...v, hasUpdate: true }));
    expect(get(lerdStatusColor)).toBe('yellow');
  });

  it('lerdStatusColor is green when healthy', async () => {
    const { status, statusLoaded, lerdStatusColor } = await import('./status');
    const { version } = await import('./version');
    statusLoaded.set(true);
    status.update((s) => ({
      ...s,
      dns: { ok: true, tld: 'test' },
      nginx: { running: true },
      watcher_running: true
    }));
    version.update((v) => ({ ...v, hasUpdate: false }));
    expect(get(lerdStatusColor)).toBe('green');
  });
});
