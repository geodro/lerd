import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { get } from 'svelte/store';

describe('version store', () => {
  const realFetch = globalThis.fetch;

  beforeEach(() => {
    vi.resetModules();
  });

  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  it('defaults to current "..." before load', async () => {
    const { version } = await import('./version');
    expect(get(version).current).toBe('...');
    expect(get(version).hasUpdate).toBe(false);
  });

  it('maps API response into the store', async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response(
        JSON.stringify({
          current: '1.18.0',
          latest: '1.19.0',
          has_update: true,
          changelog: 'stuff'
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      )
    ) as unknown as typeof fetch;
    const { version, loadVersion } = await import('./version');
    await loadVersion();
    expect(get(version)).toMatchObject({
      current: '1.18.0',
      latest: '1.19.0',
      hasUpdate: true,
      checked: true,
      changelog: 'stuff'
    });
  });

  it('tolerates fetch failure without throwing', async () => {
    globalThis.fetch = vi.fn(async () => {
      throw new Error('nope');
    }) as unknown as typeof fetch;
    const { version, loadVersion } = await import('./version');
    await loadVersion();
    expect(get(version).checking).toBe(false);
  });
});
