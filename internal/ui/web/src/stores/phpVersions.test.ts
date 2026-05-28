import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

describe('phpVersions store', () => {
  const realFetch = globalThis.fetch;

  beforeEach(() => {
    vi.resetModules();
  });

  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  it('getPhpIni GETs the per-version user ini', async () => {
    globalThis.fetch = vi.fn(
      async () =>
        new Response(JSON.stringify({ path: '/x/php/8.4/98-user.ini', content: 'memory_limit = 512M\n' }), {
          status: 200
        })
    ) as unknown as typeof fetch;
    const { getPhpIni } = await import('./phpVersions');
    const ini = await getPhpIni('8.4');
    expect(ini.path).toBe('/x/php/8.4/98-user.ini');
    expect(ini.content).toContain('memory_limit');
  });

  it('savePhpIni POSTs the content to the config endpoint', async () => {
    const calls: Array<[string, RequestInit | undefined]> = [];
    globalThis.fetch = vi.fn(async (url: unknown, init?: RequestInit) => {
      calls.push([String(url), init]);
      return new Response('{"ok":true}', { status: 200 });
    }) as unknown as typeof fetch;
    const { savePhpIni } = await import('./phpVersions');
    await expect(savePhpIni('8.4', 'opcache.memory_consumption = 256\n')).resolves.toBeUndefined();
    expect(calls[0][0]).toBe('/api/php-versions/8.4/config');
    expect(calls[0][1]?.method).toBe('POST');
    expect(JSON.parse(String(calls[0][1]?.body))).toEqual({ content: 'opcache.memory_consumption = 256\n' });
  });

  it('savePhpIni throws on non-ok with the server body as the message', async () => {
    globalThis.fetch = vi.fn(
      async () => new Response('updating php quadlet: boom\n', { status: 500, statusText: 'Internal Server Error' })
    ) as unknown as typeof fetch;
    const { savePhpIni } = await import('./phpVersions');
    await expect(savePhpIni('8.4', 'x = 1')).rejects.toThrow('updating php quadlet: boom');
  });
});
