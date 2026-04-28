import { writable } from 'svelte/store';

export type MobileView = 'tab' | 'apps';

function read(): MobileView {
  return location.hash.replace(/^#/, '') === 'apps' ? 'apps' : 'tab';
}

export const mobileView = writable<MobileView>(read());

if (typeof window !== 'undefined') {
  window.addEventListener('hashchange', () => mobileView.set(read()));
}

export function goToApps() {
  location.hash = 'apps';
}
