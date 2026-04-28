import { writable, derived, get } from 'svelte/store';
import { presets, type Preset } from './presets';
import type { Service } from './services';

const SUGGESTIONS: Record<string, string> = {
  mysql: 'phpmyadmin',
  postgres: 'pgadmin',
  mongo: 'mongo-express',
  elasticsearch: 'elasticvue'
};

const STORAGE_KEY = 'lerd-dismissed-preset-suggestions';

function readDismissed(): string[] {
  try {
    const v = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
    return Array.isArray(v) ? v : [];
  } catch {
    return [];
  }
}

export const dismissedSuggestions = writable<string[]>(readDismissed());

dismissedSuggestions.subscribe((v) => {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(v));
  } catch {
    /* no-op */
  }
});

export function dismissSuggestion(name: string) {
  dismissedSuggestions.update((list) => (list.includes(name) ? list : [...list, name]));
}

export function detectServiceFamily(svc: Service): string | null {
  if (!svc || !svc.name) return null;
  if (SUGGESTIONS[svc.name]) return svc.name;
  if (svc.connection_url) {
    const m = svc.connection_url.match(/^([a-z]+)(?:\+\w+)?:/);
    if (m) {
      const scheme = m[1];
      if (scheme === 'mysql' || scheme === 'mariadb') return 'mysql';
      if (scheme === 'postgresql' || scheme === 'postgres') return 'postgres';
      if (scheme === 'mongodb') return 'mongo';
    }
  }
  const prefix = svc.name.match(/^([a-z][a-z0-9]*?)-\d/);
  if (prefix && SUGGESTIONS[prefix[1]]) return prefix[1];
  return null;
}

export function adminServiceFor(svc: Service, services: Service[]): Service | null {
  const family = detectServiceFamily(svc);
  if (!family) return null;
  const adminName = SUGGESTIONS[family];
  if (!adminName) return null;
  return services.find((s) => s.name === adminName) || null;
}

export function suggestedPresetFor(svc: Service): Preset | null {
  if (!svc || !svc.name) return null;
  const presetName = SUGGESTIONS[svc.name];
  if (!presetName) return null;
  if (get(dismissedSuggestions).includes(presetName)) return null;
  const p = get(presets).find((x) => x.name === presetName);
  if (!p || p.installed) return null;
  return p;
}

// Reactive helper so UIs can bind to it
export const suggestionFor = (svc: Service | null | undefined) =>
  derived([presets, dismissedSuggestions], ([$presets, $dismissed]): Preset | null => {
    if (!svc || !svc.name) return null;
    const presetName = SUGGESTIONS[svc.name];
    if (!presetName) return null;
    if ($dismissed.includes(presetName)) return null;
    const p = $presets.find((x) => x.name === presetName);
    if (!p || p.installed) return null;
    return p;
  });
