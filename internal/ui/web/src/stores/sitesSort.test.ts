import { describe, it, expect, beforeEach, vi } from 'vitest';
import { get } from 'svelte/store';

describe('sitesSort', () => {
  beforeEach(() => {
    localStorage.clear();
    // Re-import fresh each test so initial() re-reads localStorage.
    vi.resetModules();
  });

  it('defaults to manual with no stored value', async () => {
    const { sitesSort } = await import('./sitesSort');
    expect(get(sitesSort)).toBe('manual');
  });

  it('restores a previously stored sort', async () => {
    localStorage.setItem('lerd:sitesSort', 'recent');
    const { sitesSort } = await import('./sitesSort');
    expect(get(sitesSort)).toBe('recent');
  });

  it('ignores a garbage stored value', async () => {
    localStorage.setItem('lerd:sitesSort', 'bogus');
    const { sitesSort } = await import('./sitesSort');
    expect(get(sitesSort)).toBe('manual');
  });

  it('persists changes to localStorage', async () => {
    const { sitesSort } = await import('./sitesSort');
    sitesSort.set('alpha');
    expect(localStorage.getItem('lerd:sitesSort')).toBe('alpha');
  });
});
