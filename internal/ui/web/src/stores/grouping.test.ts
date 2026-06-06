import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

describe('grouping store', () => {
  const realFetch = globalThis.fetch;

  beforeEach(() => {
    vi.resetModules();
  });

  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  function captureCalls(): string[] {
    const calls: string[] = [];
    globalThis.fetch = vi.fn(async (url: unknown) => {
      calls.push(String(url));
      return new Response('{"ok":true}', { status: 200 });
    }) as unknown as typeof fetch;
    return calls;
  }

  it('assignGroup posts to group:assign with secondary and label', async () => {
    const calls = captureCalls();
    const { assignGroup } = await import('./grouping');
    const r = await assignGroup({ domain: 'astrolov.test' } as never, 'admin-astrolov.test', 'admin');
    expect(r.ok).toBe(true);
    expect(calls[0]).toBe(
      '/api/sites/astrolov.test/group:assign?secondary=admin-astrolov.test&label=admin'
    );
  });

  it('assignGroup adds share_db=1 when sharing the database', async () => {
    const calls = captureCalls();
    const { assignGroup } = await import('./grouping');
    await assignGroup({ domain: 'astrolov.test' } as never, 'admin-astrolov.test', 'admin', true);
    expect(calls[0]).toBe(
      '/api/sites/astrolov.test/group:assign?secondary=admin-astrolov.test&label=admin&share_db=1'
    );
  });

  it('setGroupSharedDB posts to group:set-db', async () => {
    const calls = captureCalls();
    const { setGroupSharedDB } = await import('./grouping');
    await setGroupSharedDB({ domain: 'admin.astrolov.test' } as never, true);
    expect(calls[0]).toBe('/api/sites/admin.astrolov.test/group:set-db?share=1');
    await setGroupSharedDB({ domain: 'admin.astrolov.test' } as never, false);
    expect(calls[1]).toBe('/api/sites/admin.astrolov.test/group:set-db?share=0');
  });

  it('unassignGroup posts to group:unassign on the secondary', async () => {
    const calls = captureCalls();
    const { unassignGroup } = await import('./grouping');
    await unassignGroup({ domain: 'admin.astrolov.test' } as never);
    expect(calls[0]).toBe('/api/sites/admin.astrolov.test/group:unassign');
  });

  it('setGroupLabel posts the new label', async () => {
    const calls = captureCalls();
    const { setGroupLabel } = await import('./grouping');
    await setGroupLabel({ domain: 'admin.astrolov.test' } as never, 'backoffice');
    expect(calls[0]).toBe('/api/sites/admin.astrolov.test/group:set-label?label=backoffice');
  });

  it('dissolveGroup posts to group:remove', async () => {
    const calls = captureCalls();
    const { dissolveGroup } = await import('./grouping');
    await dissolveGroup({ domain: 'astrolov.test' } as never);
    expect(calls[0]).toBe('/api/sites/astrolov.test/group:remove');
  });

  it('returns error on non-ok', async () => {
    globalThis.fetch = vi.fn(
      async () => new Response('{"ok":false,"error":"taken"}', { status: 200 })
    ) as unknown as typeof fetch;
    const { assignGroup } = await import('./grouping');
    const r = await assignGroup({ domain: 'a.test' } as never, 'b.test', 'x');
    expect(r.ok).toBe(false);
    expect(r.error).toBe('taken');
  });
});
