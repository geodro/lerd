import { apiFetch } from '$lib/api';

export type XdebugMode =
  | 'debug'
  | 'coverage'
  | 'debug,coverage'
  | 'develop'
  | 'profile'
  | 'trace'
  | 'gcstats';

export const XDEBUG_MODES: XdebugMode[] = [
  'debug',
  'coverage',
  'debug,coverage',
  'develop',
  'profile',
  'trace',
  'gcstats'
];

async function post(path: string): Promise<{ ok: boolean; error?: string }> {
  try {
    const res = await apiFetch(path, { method: 'POST' });
    const data = (await res.json()) as { ok?: boolean; error?: string };
    return { ok: Boolean(data.ok), error: data.error };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'Request failed' };
  }
}

export function xdebugOn(version: string, mode: XdebugMode) {
  return post('/api/xdebug/' + encodeURIComponent(version) + '/on?mode=' + encodeURIComponent(mode));
}

export function xdebugOff(version: string) {
  return post('/api/xdebug/' + encodeURIComponent(version) + '/off');
}
