import { writable } from 'svelte/store';
import { getLocale, setLocale as paraglideSetLocale, locales as paraglideLocales } from '../paraglide/runtime.js';

export type Locale = (typeof paraglideLocales)[number];

export const LOCALES: readonly Locale[] = paraglideLocales;

export const LOCALE_LABELS: Record<Locale, string> = {
  en: 'English',
  es: 'Español',
  pt: 'Português',
  fr: 'Français'
};

export const LOCALE_CODES: Record<Locale, string> = {
  en: 'EN',
  es: 'ES',
  pt: 'PT',
  fr: 'FR'
};

// Reactive store mirroring Paraglide's active locale. Paraglide stores the
// choice in localStorage via its configured strategy (localStorage →
// preferredLanguage → baseLocale), so this wrapper just surfaces it to Svelte
// for reactive re-renders when the user picks a new language.
export const locale = writable<Locale>(getLocale() as Locale);

export function changeLocale(next: Locale) {
  // `reload: false` would ask Paraglide to swap without a page reload, but
  // messages are module-scoped functions compiled per-locale so everything
  // mounted keeps the previous value until the component re-evaluates. A full
  // reload is the safe path; the whole app boots in < 200 ms.
  paraglideSetLocale(next, { reload: true });
  locale.set(next);
}
