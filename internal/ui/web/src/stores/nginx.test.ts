import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

describe('nginx store', () => {
  const realFetch = globalThis.fetch;

  beforeEach(() => {
    vi.resetModules();
  });

  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  it('getNginxConfig GETs the global override', async () => {
    globalThis.fetch = vi.fn(
      async () => new Response(JSON.stringify({ path: '/x/http.d/zz-lerd-user.conf', content: 'gzip on;\n' }), { status: 200 })
    ) as unknown as typeof fetch;
    const { getNginxConfig } = await import('./nginx');
    const cfg = await getNginxConfig();
    expect(cfg.path).toContain('zz-lerd-user.conf');
    expect(cfg.content).toContain('gzip on;');
  });

  it('saveNginxConfig POSTs the content to /api/nginx/config', async () => {
    const calls: Array<[string, RequestInit | undefined]> = [];
    globalThis.fetch = vi.fn(async (url: unknown, init?: RequestInit) => {
      calls.push([String(url), init]);
      return new Response('{"ok":true}', { status: 200 });
    }) as unknown as typeof fetch;
    const { saveNginxConfig } = await import('./nginx');
    const ok = await saveNginxConfig('client_max_body_size 100m;\n');
    expect(ok).toBe(true);
    expect(calls[0][0]).toBe('/api/nginx/config');
    expect(calls[0][1]?.method).toBe('POST');
    expect(JSON.parse(String(calls[0][1]?.body))).toEqual({ content: 'client_max_body_size 100m;\n' });
  });
});
