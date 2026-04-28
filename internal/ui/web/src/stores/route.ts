import { writable, derived } from 'svelte/store';

export type TabId = 'sites' | 'services' | 'system';

export const TABS: TabId[] = ['sites', 'services', 'system'];

export function parseHash(hash: string): { tab: TabId; rest: string } {
  const h = hash.startsWith('#') ? hash.slice(1) : hash;
  for (const t of TABS) {
    if (h === t) return { tab: t, rest: '' };
    if (h.startsWith(t + '/')) return { tab: t, rest: h.slice(t.length + 1) };
  }
  return { tab: 'sites', rest: '' };
}

const initial = parseHash(location.hash);
export const tab = writable<TabId>(initial.tab);
export const routeRest = writable<string>(initial.rest);

export const route = derived([tab, routeRest], ([$tab, $rest]) => ({ tab: $tab, rest: $rest }));

window.addEventListener('hashchange', () => {
  const p = parseHash(location.hash);
  tab.set(p.tab);
  routeRest.set(p.rest);
});

export function goToTab(t: TabId, rest = '') {
  const h = rest ? `${t}/${rest}` : t;
  location.hash = h;
}
