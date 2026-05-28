import { writable } from 'svelte/store';
import { apiJson, apiFetch } from '$lib/api';

export const phpVersions = writable<string[]>([]);

export async function loadPhpVersions() {
  try {
    const list = await apiJson<string[]>('/api/php-versions');
    phpVersions.set(Array.isArray(list) ? list : []);
  } catch {
    /* keep previous */
  }
}

async function phpAction(v: string, action: 'set-default' | 'start' | 'stop' | 'remove'): Promise<boolean> {
  try {
    const res = await apiFetch('/api/php-versions/' + encodeURIComponent(v) + '/' + action, {
      method: 'POST'
    });
    return res.ok;
  } catch {
    return false;
  }
}

export const setDefaultPhp = (v: string) => phpAction(v, 'set-default');
export const startPhp = (v: string) => phpAction(v, 'start');
export const stopPhp = (v: string) => phpAction(v, 'stop');
export const removePhp = (v: string) => phpAction(v, 'remove');

export interface PhpIni {
  path: string;
  content: string;
}

export async function getPhpIni(v: string): Promise<PhpIni> {
  return apiJson<PhpIni>('/api/php-versions/' + encodeURIComponent(v) + '/config');
}

export async function savePhpIni(v: string, content: string): Promise<void> {
  const res = await apiFetch('/api/php-versions/' + encodeURIComponent(v) + '/config', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ content })
  });
  if (!res.ok) {
    // Surface the server's reason (apiFetch swallows nothing) so callers can
    // render a specific error instead of a generic "failed". Mirror getPhpIni
    // which already throws on non-ok via apiJson.
    const body = await res.text().catch(() => '');
    throw new Error(body.trim() || `${res.status} ${res.statusText}`);
  }
}
