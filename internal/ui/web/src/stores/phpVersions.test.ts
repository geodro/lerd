import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { phpOptionsForSite } from './phpVersions';

describe('phpOptionsForSite', () => {
  const installed = ['7.4', '8.1', '8.3', '8.4', '8.5'];
  const franken = ['8.2', '8.3', '8.4', '8.5'];

  it('returns every installed version for non-FrankenPHP runtimes', () => {
    expect(phpOptionsForSite('fpm', installed, franken, '8.4')).toEqual(installed);
    expect(phpOptionsForSite(undefined, installed, franken, '8.4')).toEqual(installed);
  });

  it('limits FrankenPHP to installed versions that are also publishable', () => {
    expect(phpOptionsForSite('frankenphp', installed, franken, '8.4')).toEqual(['8.3', '8.4', '8.5']);
  });

  it('keeps the current version even when it is not FPM-installed', () => {
    expect(phpOptionsForSite('frankenphp', ['8.3', '8.4'], franken, '8.5')).toEqual(['8.3', '8.4', '8.5']);
  });

  it('never offers a version FrankenPHP cannot run', () => {
    expect(phpOptionsForSite('frankenphp', installed, franken, '8.4')).not.toContain('7.4');
    expect(phpOptionsForSite('frankenphp', installed, franken, '8.4')).not.toContain('8.1');
  });
});

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
        new Response(JSON.stringify({ path: '/x/php/8.4/98-user.ini', content: 'memory_limit = 512M\n', exists: true }), {
          status: 200
        })
    ) as unknown as typeof fetch;
    const { getPhpIni } = await import('./phpVersions');
    const ini = await getPhpIni('8.4');
    expect(ini.path).toBe('/x/php/8.4/98-user.ini');
    expect(ini.content).toContain('memory_limit');
    expect(ini.exists).toBe(true);
  });

  it('savePhpIni POSTs the content to the config endpoint and returns the result', async () => {
    const calls: Array<[string, RequestInit | undefined]> = [];
    globalThis.fetch = vi.fn(async (url: unknown, init?: RequestInit) => {
      calls.push([String(url), init]);
      return new Response('{"ok":true,"content":"opcache.memory_consumption = 256\\n","exists":true,"backup_name":"98-user.ini.bkp.20260528-220000"}', { status: 200 });
    }) as unknown as typeof fetch;
    const { savePhpIni } = await import('./phpVersions');
    const res = await savePhpIni('8.4', 'opcache.memory_consumption = 256\n', true);
    expect(res.ok).toBe(true);
    expect(res.content).toBe('opcache.memory_consumption = 256\n');
    expect(res.backupName).toBe('98-user.ini.bkp.20260528-220000');
    expect(calls[0][0]).toBe('/api/php-versions/8.4/config');
    expect(calls[0][1]?.method).toBe('POST');
    expect(JSON.parse(String(calls[0][1]?.body))).toEqual({ content: 'opcache.memory_consumption = 256\n', backup: true });
  });

  it('savePhpIni surfaces the server error message on failure', async () => {
    globalThis.fetch = vi.fn(
      async () => new Response('{"ok":false,"error":"updating php quadlet: boom"}', { status: 200 })
    ) as unknown as typeof fetch;
    const { savePhpIni } = await import('./phpVersions');
    const res = await savePhpIni('8.4', 'x = 1');
    expect(res.ok).toBe(false);
    expect(res.error).toBe('updating php quadlet: boom');
  });
});
