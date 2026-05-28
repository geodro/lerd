import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

describe('sites actions', () => {
  const realFetch = globalThis.fetch;

  beforeEach(() => {
    vi.resetModules();
  });

  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  it('pauseSite POSTs /pause', async () => {
    const calls: string[] = [];
    globalThis.fetch = vi.fn(async (url: unknown) => {
      calls.push(String(url));
      return new Response('{"ok":true}', { status: 200 });
    }) as unknown as typeof fetch;
    const { pauseSite } = await import('./sites');
    const r = await pauseSite('a.test');
    expect(r.ok).toBe(true);
    expect(calls[0]).toBe('/api/sites/a.test/pause');
  });

  it('getSiteNginx GETs the per-site override', async () => {
    globalThis.fetch = vi.fn(
      async () => new Response(JSON.stringify({ path: '/x/custom.d/a.test.conf', content: '# snippet\n' }), { status: 200 })
    ) as unknown as typeof fetch;
    const { getSiteNginx } = await import('./sites');
    const res = await getSiteNginx('a.test');
    expect(res.path).toContain('a.test.conf');
    expect(res.content).toContain('# snippet');
  });

  it('saveSiteNginx POSTs the content to /nginx', async () => {
    const calls: Array<[string, RequestInit | undefined]> = [];
    globalThis.fetch = vi.fn(async (url: unknown, init?: RequestInit) => {
      calls.push([String(url), init]);
      return new Response('{"ok":true}', { status: 200 });
    }) as unknown as typeof fetch;
    const { saveSiteNginx } = await import('./sites');
    const ok = await saveSiteNginx('a.test', 'client_max_body_size 100m;\n');
    expect(ok).toBe(true);
    expect(calls[0][0]).toBe('/api/sites/a.test/nginx');
    expect(calls[0][1]?.method).toBe('POST');
    expect(JSON.parse(String(calls[0][1]?.body))).toEqual({ content: 'client_max_body_size 100m;\n' });
  });

  it('resumeSite POSTs /unpause', async () => {
    const calls: string[] = [];
    globalThis.fetch = vi.fn(async (url: unknown) => {
      calls.push(String(url));
      return new Response('{"ok":true}', { status: 200 });
    }) as unknown as typeof fetch;
    const { resumeSite } = await import('./sites');
    await resumeSite('a.test');
    expect(calls[0]).toBe('/api/sites/a.test/unpause');
  });

  it('toggleTLS flips between secure/unsecure', async () => {
    const calls: string[] = [];
    globalThis.fetch = vi.fn(async (url: unknown) => {
      calls.push(String(url));
      return new Response('{"ok":true}', { status: 200 });
    }) as unknown as typeof fetch;
    const { toggleTLS } = await import('./sites');
    await toggleTLS({ domain: 'a.test', tls: false });
    await toggleTLS({ domain: 'a.test', tls: true });
    expect(calls[0]).toBe('/api/sites/a.test/secure');
    expect(calls[1]).toBe('/api/sites/a.test/unsecure');
  });

  it('toggleQueue uses running state', async () => {
    const calls: string[] = [];
    globalThis.fetch = vi.fn(async (url: unknown) => {
      calls.push(String(url));
      return new Response('{"ok":true}', { status: 200 });
    }) as unknown as typeof fetch;
    const { toggleQueue } = await import('./sites');
    await toggleQueue({ domain: 'a.test', queue_running: false });
    await toggleQueue({ domain: 'a.test', queue_running: true });
    expect(calls[0]).toBe('/api/sites/a.test/queue:start');
    expect(calls[1]).toBe('/api/sites/a.test/queue:stop');
  });

  it('setSiteVersion encodes version in query', async () => {
    const calls: string[] = [];
    globalThis.fetch = vi.fn(async (url: unknown) => {
      calls.push(String(url));
      return new Response('{"ok":true}', { status: 200 });
    }) as unknown as typeof fetch;
    const { setSiteVersion } = await import('./sites');
    await setSiteVersion({ domain: 'a.test' }, 'php', '8.5');
    expect(calls[0]).toBe('/api/sites/a.test/php?version=8.5');
  });

  it('fpmContainer handles custom/frankenphp/normal', async () => {
    const { fpmContainer } = await import('./sites');
    expect(fpmContainer({ domain: 'a.test', name: 'a', custom_container: true })).toBe('lerd-custom-a');
    expect(fpmContainer({ domain: 'a.test', name: 'a', runtime: 'frankenphp' })).toBe('lerd-fp-a');
    expect(fpmContainer({ domain: 'a.test', php_version: '8.4' })).toBe('lerd-php84-fpm');
  });

  it('siteWorkerFailing checks any worker field', async () => {
    const { siteWorkerFailing } = await import('./sites');
    expect(siteWorkerFailing({ domain: 'a', queue_failing: true })).toBe(true);
    expect(siteWorkerFailing({ domain: 'a', framework_workers: [{ name: 'x', failing: true }] })).toBe(true);
    expect(siteWorkerFailing({ domain: 'a' })).toBe(false);
  });
});
