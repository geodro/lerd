import { describe, it, expect, beforeEach, vi } from 'vitest';
import { get } from 'svelte/store';

describe('sites store', () => {
  beforeEach(() => {
    vi.resetModules();
  });

  it('derives php and node site counts', async () => {
    const { sites, sitesByPhp, sitesByNode, phpSiteCount, nodeSiteCount } = await import('./sites');
    sites.set([
      { domain: 'a.test', php_version: '8.4', node_version: '22' },
      { domain: 'b.test', php_version: '8.5', node_version: '22' },
      { domain: 'c.test', php_version: '8.5' }
    ]);
    expect(get(sitesByPhp).get('8.5')).toBe(2);
    expect(get(sitesByPhp).get('8.4')).toBe(1);
    expect(phpSiteCount('8.5')).toBe(2);
    expect(get(sitesByNode).get('22')).toBe(2);
    expect(nodeSiteCount('22')).toBe(2);
    expect(nodeSiteCount('24')).toBe(0);
  });

  it('activeWorktreeDomain returns the parent domain when branch is empty', async () => {
    const { activeWorktreeDomain } = await import('./sites');
    const s = {
      domain: 'acme.test',
      worktrees: [{ branch: 'feat-a', domain: 'feat-a.acme.test' }]
    };
    expect(activeWorktreeDomain(s, '')).toBe('acme.test');
  });

  it('activeWorktreeDomain returns the worktree domain when branch matches', async () => {
    const { activeWorktreeDomain } = await import('./sites');
    const s = {
      domain: 'acme.test',
      worktrees: [{ branch: 'feat-a', domain: 'feat-a.acme.test' }]
    };
    expect(activeWorktreeDomain(s, 'feat-a')).toBe('feat-a.acme.test');
  });

  it('activeWorktreeDomain falls back to the parent when branch is unknown', async () => {
    const { activeWorktreeDomain } = await import('./sites');
    const s = { domain: 'acme.test', worktrees: [] };
    expect(activeWorktreeDomain(s, 'mystery')).toBe('acme.test');
  });

  it('saveSiteEnv PUTs JSON body to the site env endpoint', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ ok: true, backup_path: '.env.20260528-103045' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      })
    );
    vi.stubGlobal('fetch', fetchMock);
    const { saveSiteEnv } = await import('./sites');

    const res = await saveSiteEnv('acme.test', '', 'FOO=bar\n', true);

    expect(res.ok).toBe(true);
    expect(res.backupPath).toBe('.env.20260528-103045');
    expect(fetchMock).toHaveBeenCalledOnce();
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toMatch(/\/api\/sites\/acme\.test\/env$/);
    expect(init.method).toBe('PUT');
    expect(init.headers).toMatchObject({ 'Content-Type': 'application/json' });
    expect(JSON.parse(init.body as string)).toEqual({ content: 'FOO=bar\n', backup: true });
    vi.unstubAllGlobals();
  });

  it('saveSiteEnv appends branch query param when set', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } })
    );
    vi.stubGlobal('fetch', fetchMock);
    const { saveSiteEnv } = await import('./sites');

    await saveSiteEnv('acme.test', 'feature/x', 'A=1', false);

    const [url] = fetchMock.mock.calls[0];
    expect(String(url)).toMatch(/\/api\/sites\/acme\.test\/env\?branch=feature%2Fx$/);
    vi.unstubAllGlobals();
  });

  it('loadSiteEnvFiles GETs /env/files and returns the parsed list', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(['.env', '.env.local', '.env.testing']), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      })
    );
    vi.stubGlobal('fetch', fetchMock);
    const { loadSiteEnvFiles } = await import('./sites');

    const list = await loadSiteEnvFiles('acme.test', '');

    expect(list).toEqual(['.env', '.env.local', '.env.testing']);
    const [url] = fetchMock.mock.calls[0];
    expect(String(url)).toMatch(/\/api\/sites\/acme\.test\/env\/files$/);
    vi.unstubAllGlobals();
  });

  it('loadSiteEnvFiles falls back to [.env] on error or empty list', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 500 })));
    const { loadSiteEnvFiles } = await import('./sites');
    expect(await loadSiteEnvFiles('acme.test', '')).toEqual(['.env']);
    vi.unstubAllGlobals();
  });

  it('saveSiteEnv appends ?file= when targeting a non-default env file', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ ok: true, backup_path: '.env.testing.bkp.20260528-103045' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      })
    );
    vi.stubGlobal('fetch', fetchMock);
    const { saveSiteEnv } = await import('./sites');

    await saveSiteEnv('acme.test', '', 'X=1\n', true, '.env.testing');

    const [url] = fetchMock.mock.calls[0];
    expect(String(url)).toMatch(/\/api\/sites\/acme\.test\/env\?file=\.env\.testing$/);
    vi.unstubAllGlobals();
  });

  it('saveSiteEnv omits ?file= for the default .env to keep URLs stable', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } })
    );
    vi.stubGlobal('fetch', fetchMock);
    const { saveSiteEnv } = await import('./sites');

    await saveSiteEnv('acme.test', '', 'X=1\n', false);

    const [url] = fetchMock.mock.calls[0];
    expect(String(url)).toMatch(/\/api\/sites\/acme\.test\/env$/);
    vi.unstubAllGlobals();
  });

  it('loadSiteEnvBackups GETs /env/backups and returns the parsed list', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify([
          { name: '.env.20260528-103045', mtime_unix: 1779973000 },
          { name: '.env.20260101-100000', mtime_unix: 1767369600 }
        ]),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      )
    );
    vi.stubGlobal('fetch', fetchMock);
    const { loadSiteEnvBackups } = await import('./sites');

    const list = await loadSiteEnvBackups('acme.test', '');

    expect(list).toHaveLength(2);
    expect(list[0].name).toBe('.env.20260528-103045');
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toMatch(/\/api\/sites\/acme\.test\/env\/backups$/);
    expect(init?.method).toBeUndefined();
    vi.unstubAllGlobals();
  });

  it('loadSiteEnvBackups returns an empty list on error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 500 })));
    const { loadSiteEnvBackups } = await import('./sites');
    expect(await loadSiteEnvBackups('acme.test', '')).toEqual([]);
    vi.unstubAllGlobals();
  });

  it('restoreSiteEnv POSTs to /env/restore and returns parsed payload', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ ok: true, restored: '.env.20260528-103045', content: 'OLD=1\n' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      })
    );
    vi.stubGlobal('fetch', fetchMock);
    const { restoreSiteEnv } = await import('./sites');

    const res = await restoreSiteEnv('acme.test', 'feature/x');

    expect(res.ok).toBe(true);
    expect(res.restored).toBe('.env.20260528-103045');
    expect(res.content).toBe('OLD=1\n');
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toMatch(/\/api\/sites\/acme\.test\/env\/restore\?branch=feature%2Fx$/);
    expect(init.method).toBe('POST');
    vi.unstubAllGlobals();
  });

  it('saveSiteEnv surfaces backend errors verbatim', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ ok: false, error: 'writing temp file: no space' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' }
        })
      )
    );
    const { saveSiteEnv } = await import('./sites');

    const res = await saveSiteEnv('acme.test', '', '', true);

    expect(res.ok).toBe(false);
    expect(res.error).toBe('writing temp file: no space');
    vi.unstubAllGlobals();
  });
});
