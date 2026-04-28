import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { get } from 'svelte/store';

describe('services store', () => {
  const realFetch = globalThis.fetch;

  beforeEach(() => {
    vi.resetModules();
  });

  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  it('splits services into core and worker groups', async () => {
    const { services, coreServices, workerGroups } = await import('./services');
    services.set([
      { name: 'mysql', status: 'active', site_count: 1 },
      { name: 'queue-foo', status: 'active', site_count: 0, queue_site: 'foo' },
      { name: 'horizon-bar', status: 'active', site_count: 0, horizon_site: 'bar' },
      { name: 'redis', status: 'inactive', site_count: 2 }
    ]);
    const core = get(coreServices);
    expect(core.map((s) => s.name)).toEqual(['mysql', 'redis']);

    const groups = get(workerGroups);
    expect(groups.find((g) => g.key === 'queue')?.items[0].name).toBe('queue-foo');
    expect(groups.find((g) => g.key === 'horizon')?.items[0].name).toBe('horizon-bar');
    expect(groups.find((g) => g.key === 'schedule')).toBeUndefined();
  });

  it('applies ws service frames', async () => {
    const { wsMessage } = await import('$lib/ws');
    const { services, servicesLoaded } = await import('./services');
    wsMessage.set({ type: 'services', services: [{ name: 'x', status: 'active', site_count: 0 }] });
    expect(get(services)[0].name).toBe('x');
    expect(get(servicesLoaded)).toBe(true);
  });

  it('serviceAction POSTs to the correct URL and reloads', async () => {
    const calls: Array<[string, RequestInit | undefined]> = [];
    globalThis.fetch = vi.fn(async (url: unknown, init?: RequestInit) => {
      calls.push([String(url), init]);
      if (String(url).endsWith('/mysql/stop')) return new Response('{}', { status: 200 });
      return new Response('[]', { status: 200 });
    }) as unknown as typeof fetch;
    const { serviceAction } = await import('./services');
    const ok = await serviceAction('mysql', 'stop');
    expect(ok).toBe(true);
    expect(calls[0][0]).toBe('/api/services/mysql/stop');
    expect(calls[0][1]?.method).toBe('POST');
    // Second call should be the reload
    expect(calls.some((c) => c[0] === '/api/services')).toBe(true);
  });

  it('serviceLabel handles overrides, versioned names, and fallbacks', async () => {
    const { serviceLabel } = await import('./services');
    expect(serviceLabel('mysql')).toBe('MySQL');
    expect(serviceLabel('mysql-5-7')).toBe('MySQL');
    expect(serviceLabel('stripe-mock')).toBe('Stripe Mock');
    expect(serviceLabel('custom-thing')).toBe('Custom Thing');
  });

  it('detailLabel names worker roles', async () => {
    const { detailLabel } = await import('./services');
    expect(detailLabel({ name: 'queue-a', status: 'active', site_count: 0, queue_site: 'a' })).toBe(
      'Queue worker'
    );
    expect(detailLabel({ name: 'horizon-b', status: 'active', site_count: 0, horizon_site: 'b' })).toBe(
      'Horizon'
    );
    expect(detailLabel({ name: 'mysql', status: 'active', site_count: 0 })).toBe('MySQL');
  });

  it('parentSiteDomain appends .test', async () => {
    const { parentSiteDomain } = await import('./services');
    expect(parentSiteDomain({ name: 'queue-foo', status: 'active', site_count: 0, queue_site: 'foo' })).toBe(
      'foo.test'
    );
    expect(parentSiteDomain({ name: 'mysql', status: 'active', site_count: 0 })).toBeNull();
  });
});
