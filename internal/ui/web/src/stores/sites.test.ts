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
});
