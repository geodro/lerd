import { writable } from 'svelte/store';

export type Theme = 'light' | 'dark' | 'auto';

const KEY = 'lerd-theme';

function read(): Theme {
  const v = localStorage.getItem(KEY);
  return v === 'light' || v === 'dark' || v === 'auto' ? v : 'auto';
}

function apply(theme: Theme) {
  const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
  const dark = theme === 'dark' || (theme === 'auto' && prefersDark);
  document.documentElement.classList.toggle('dark', dark);
}

export const theme = writable<Theme>('auto');

export function initTheme() {
  const initial = read();
  theme.set(initial);
  apply(initial);
  theme.subscribe((t) => {
    localStorage.setItem(KEY, t);
    apply(t);
  });
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
    theme.update((t) => t);
  });
}
