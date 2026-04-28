import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

describe('domains store', () => {
  const realFetch = globalThis.fetch;

  beforeEach(() => {
    vi.resetModules();
  });

  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  it('addDomain posts to /domain:add with name', async () => {
    const calls: string[] = [];
    globalThis.fetch = vi.fn(async (url: unknown) => {
      calls.push(String(url));
      return new Response('{"ok":true}', { status: 200 });
    }) as unknown as typeof fetch;
    const { addDomain } = await import('./domains');
    const r = await addDomain({ domain: 'a.test' } as never, 'www');
    expect(r.ok).toBe(true);
    expect(calls[0]).toBe('/api/sites/a.test/domain:add?name=www');
  });

  it('editDomain passes old and new', async () => {
    const calls: string[] = [];
    globalThis.fetch = vi.fn(async (url: unknown) => {
      calls.push(String(url));
      return new Response('{"ok":true}', { status: 200 });
    }) as unknown as typeof fetch;
    const { editDomain } = await import('./domains');
    await editDomain({ domain: 'a.test' } as never, 'old', 'new');
    expect(calls[0]).toBe('/api/sites/a.test/domain:edit?old=old&new=new');
  });

  it('removeDomain passes name', async () => {
    const calls: string[] = [];
    globalThis.fetch = vi.fn(async (url: unknown) => {
      calls.push(String(url));
      return new Response('{"ok":true}', { status: 200 });
    }) as unknown as typeof fetch;
    const { removeDomain } = await import('./domains');
    await removeDomain({ domain: 'a.test' } as never, 'foo');
    expect(calls[0]).toBe('/api/sites/a.test/domain:remove?name=foo');
  });

  it('returns error on non-ok', async () => {
    globalThis.fetch = vi.fn(async () => new Response('{"ok":false,"error":"taken"}', { status: 200 })) as unknown as typeof fetch;
    const { addDomain } = await import('./domains');
    const r = await addDomain({ domain: 'a.test' } as never, 'x');
    expect(r.ok).toBe(false);
    expect(r.error).toBe('taken');
  });
});
