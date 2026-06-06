import { writable } from 'svelte/store';

// Remembers how the user wants the Sites list ordered, so a refresh keeps the
// same view. 'manual' is the registry order from sites.yaml (drag-reorderable);
// the others are display-only sorts computed in SitesTab.
export type SitesSort = 'manual' | 'recent' | 'alpha' | 'newest';

const KEY = 'lerd:sitesSort';

const VALID: SitesSort[] = ['manual', 'recent', 'alpha', 'newest'];

function initial(): SitesSort {
  if (typeof localStorage === 'undefined') return 'manual';
  const v = localStorage.getItem(KEY) as SitesSort | null;
  return v && VALID.includes(v) ? v : 'manual';
}

export const sitesSort = writable<SitesSort>(initial());

sitesSort.subscribe((v) => {
  try {
    if (typeof localStorage !== 'undefined') localStorage.setItem(KEY, v);
  } catch {
    // private mode / storage disabled — fall back to in-memory only.
  }
});
