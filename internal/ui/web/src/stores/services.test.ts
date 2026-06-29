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

  it('setServicePorts POSTs the published port and extra ports as JSON', async () => {
    const calls: Array<[string, RequestInit | undefined]> = [];
    globalThis.fetch = vi.fn(async (url: unknown, init?: RequestInit) => {
      calls.push([String(url), init]);
      if (String(url).endsWith('/mysql/ports'))
        return new Response(JSON.stringify({ ok: true }), { status: 200 });
      return new Response('[]', { status: 200 });
    }) as unknown as typeof fetch;
    const { setServicePorts } = await import('./services');
    const res = await setServicePorts('mysql', { published_port: 3307, extra_ports: ['8080:80'] });
    expect(res.ok).toBe(true);
    expect(calls[0][0]).toBe('/api/services/mysql/ports');
    expect(calls[0][1]?.method).toBe('POST');
    expect(JSON.parse(String(calls[0][1]?.body))).toEqual({
      published_port: 3307,
      extra_ports: ['8080:80']
    });
    expect(calls.some((c) => c[0] === '/api/services')).toBe(true);
  });

  it('setServicePorts surfaces a server error without reloading', async () => {
    const calls: string[] = [];
    globalThis.fetch = vi.fn(async (url: unknown) => {
      calls.push(String(url));
      return new Response(JSON.stringify({ ok: false, error: 'port already in use: 3307' }), {
        status: 200
      });
    }) as unknown as typeof fetch;
    const { setServicePorts } = await import('./services');
    const res = await setServicePorts('mysql', { published_port: 3307, extra_ports: [] });
    expect(res.ok).toBe(false);
    expect(res.error).toContain('already in use');
    expect(calls.some((c) => c === '/api/services')).toBe(false);
  });

  it('getServiceConfig GETs the tuning override with exists flag', async () => {
    globalThis.fetch = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            supported: true,
            target: '/etc/mysql/conf.d/zz-lerd-user.cnf',
            content: '[mysqld]\n',
            exists: true
          }),
          { status: 200 }
        )
    ) as unknown as typeof fetch;
    const { getServiceConfig } = await import('./services');
    const cfg = await getServiceConfig('mariadb-10-11');
    expect(cfg.supported).toBe(true);
    expect(cfg.target).toBe('/etc/mysql/conf.d/zz-lerd-user.cnf');
    expect(cfg.content).toContain('[mysqld]');
    expect(cfg.exists).toBe(true);
  });

  it('saveServiceConfig POSTs the content + backup flag without an extra /api/services round-trip', async () => {
    const calls: Array<[string, RequestInit | undefined]> = [];
    globalThis.fetch = vi.fn(async (url: unknown, init?: RequestInit) => {
      calls.push([String(url), init]);
      return new Response(
        JSON.stringify({ ok: true, content: '[mysqld]\nmax_allowed_packet = 1G\n', exists: true }),
        { status: 200 }
      );
    }) as unknown as typeof fetch;
    const { saveServiceConfig } = await import('./services');
    const res = await saveServiceConfig('mariadb-10-11', '[mysqld]\nmax_allowed_packet = 1G\n', true);
    expect(res.ok).toBe(true);
    expect(res.content).toContain('max_allowed_packet');
    expect(res.exists).toBe(true);
    expect(calls[0][0]).toBe('/api/services/mariadb-10-11/config');
    expect(calls[0][1]?.method).toBe('POST');
    expect(JSON.parse(String(calls[0][1]?.body))).toEqual({
      content: '[mysqld]\nmax_allowed_packet = 1G\n',
      backup: true
    });
    // The handler's publishAfter middleware already broadcasts a
    // KindServices WS frame that drives the same refresh; the store
    // no longer also issues GET /api/services in a finally block.
    expect(calls.some((c) => c[0] === '/api/services')).toBe(false);
  });

  it('saveServiceConfig surfaces 404 not-installed text as error', async () => {
    globalThis.fetch = vi.fn(
      async () =>
        new Response('service "mysql" is not installed — run `lerd service preset install mysql` first\n', {
          status: 404,
          statusText: 'Not Found'
        })
    ) as unknown as typeof fetch;
    const { saveServiceConfig } = await import('./services');
    const res = await saveServiceConfig('mysql', 'x = 1');
    expect(res.ok).toBe(false);
    expect(res.error).toContain('is not installed');
  });

  it('saveServiceConfig surfaces rolled_back content for the auto-rollback path', async () => {
    globalThis.fetch = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            ok: false,
            error: 'service mysql did not become ready',
            rolled_back: true,
            content: '[mysqld]\n# previous good content\n',
            exists: true
          }),
          { status: 200 }
        )
    ) as unknown as typeof fetch;
    const { saveServiceConfig } = await import('./services');
    const res = await saveServiceConfig('mysql', 'oops', false);
    expect(res.ok).toBe(false);
    expect(res.rolledBack).toBe(true);
    expect(res.content).toContain('previous good content');
  });

  it('loadServiceTuningBackups returns ok=true with the list', async () => {
    globalThis.fetch = vi.fn(
      async () =>
        new Response('[{"name":"mysql.conf.bkp.20260528-101010","mtime_unix":1}]', { status: 200 })
    ) as unknown as typeof fetch;
    const { loadServiceTuningBackups } = await import('./services');
    const r = await loadServiceTuningBackups('mysql');
    expect(r.ok).toBe(true);
    expect(r.list[0].name).toContain('bkp');
  });

  it('loadServiceTuningBackups returns ok=false with error on 500', async () => {
    globalThis.fetch = vi.fn(
      async () => new Response('internal error', { status: 500 })
    ) as unknown as typeof fetch;
    const { loadServiceTuningBackups } = await import('./services');
    const r = await loadServiceTuningBackups('mysql');
    expect(r.ok).toBe(false);
    expect(r.error).toMatch(/500/);
  });

  it('restoreServiceTuning POSTs the named backup to /config/restore', async () => {
    const calls: Array<[string, RequestInit | undefined]> = [];
    globalThis.fetch = vi.fn(async (url: unknown, init?: RequestInit) => {
      calls.push([String(url), init]);
      return new Response(
        JSON.stringify({ ok: true, restored: 'mysql.conf.bkp.20260528-101010', content: '# old\n' }),
        { status: 200 }
      );
    }) as unknown as typeof fetch;
    const { restoreServiceTuning } = await import('./services');
    const r = await restoreServiceTuning('mysql', 'mysql.conf.bkp.20260528-101010');
    expect(calls[0][0]).toBe('/api/services/mysql/config/restore');
    expect(JSON.parse(String(calls[0][1]?.body))).toEqual({ name: 'mysql.conf.bkp.20260528-101010' });
    expect(r.ok).toBe(true);
    expect(r.restored).toContain('bkp');
  });

  it('resetServiceTuning POSTs to /config/reset', async () => {
    const calls: string[] = [];
    globalThis.fetch = vi.fn(async (url: unknown) => {
      calls.push(String(url));
      return new Response('{"ok":true}', { status: 200 });
    }) as unknown as typeof fetch;
    const { resetServiceTuning } = await import('./services');
    const r = await resetServiceTuning('mysql');
    expect(calls[0]).toBe('/api/services/mysql/config/reset');
    expect(r.ok).toBe(true);
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

  it('parentSiteDomain resolves to the registered site domain', async () => {
    const { sites } = await import('./sites');
    const { parentSiteDomain } = await import('./services');
    sites.set([{ name: 'foo', domain: 'foo.test' }]);
    expect(parentSiteDomain({ name: 'queue-foo', status: 'active', site_count: 0, queue_site: 'foo' })).toBe(
      'foo.test'
    );
    expect(parentSiteDomain({ name: 'mysql', status: 'active', site_count: 0 })).toBeNull();
  });
});
