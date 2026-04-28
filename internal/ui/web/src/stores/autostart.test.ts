import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { get } from 'svelte/store';

describe('autostart store', () => {
  const realFetch = globalThis.fetch;

  beforeEach(() => {
    vi.resetModules();
  });

  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  it('loads from /api/settings', async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response(JSON.stringify({ autostart_on_login: true }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      })
    ) as unknown as typeof fetch;
    const { autostartEnabled, loadAutostart } = await import('./autostart');
    await loadAutostart();
    expect(get(autostartEnabled)).toBe(true);
  });

  it('toggleAutostart POSTs and flips store on success', async () => {
    const fetchMock = vi.fn(async () => new Response('{}', { status: 200 }));
    globalThis.fetch = fetchMock as unknown as typeof fetch;
    const { autostartEnabled, toggleAutostart } = await import('./autostart');
    expect(get(autostartEnabled)).toBe(false);
    const ok = await toggleAutostart(true);
    expect(ok).toBe(true);
    expect(get(autostartEnabled)).toBe(true);
    expect(fetchMock).toHaveBeenCalledOnce();
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/settings/autostart');
    expect(init.method).toBe('POST');
    expect(init.body).toBe(JSON.stringify({ enabled: true }));
  });

  it('does not flip on failure', async () => {
    globalThis.fetch = vi.fn(async () => new Response('nope', { status: 500 })) as unknown as typeof fetch;
    const { autostartEnabled, toggleAutostart } = await import('./autostart');
    const ok = await toggleAutostart(true);
    expect(ok).toBe(false);
    expect(get(autostartEnabled)).toBe(false);
  });
});
