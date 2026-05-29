import { writable } from 'svelte/store';
import { apiJson, apiFetch, decodeJSONResult } from '$lib/api';
import type { SiteNginxBackup, LoadNginxBackupsResult, ResetNginxResult, SaveNginxResult, RestoreNginxResult } from './sites';

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
  exists: boolean;
}

function phpConfigUrl(v: string, suffix: string = ''): string {
  return '/api/php-versions/' + encodeURIComponent(v) + '/config' + (suffix ? '/' + suffix : '');
}

export async function getPhpIni(v: string): Promise<PhpIni> {
  return apiJson<PhpIni>(phpConfigUrl(v));
}

export async function savePhpIni(v: string, content: string, backup: boolean = false): Promise<SaveNginxResult> {
  try {
    const res = await apiFetch(phpConfigUrl(v), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content, backup })
    });
    const data = await decodeJSONResult<{
      ok?: boolean;
      error?: string;
      backup_name?: string;
      content?: string;
      exists?: boolean;
    }>(res);
    return {
      ok: Boolean(data.ok),
      error: data.error,
      backupName: data.backup_name,
      content: data.content,
      exists: data.exists
    };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'Request failed' };
  }
}

export async function loadPhpIniBackups(v: string): Promise<LoadNginxBackupsResult> {
  try {
    const res = await apiFetch(phpConfigUrl(v, 'backups'));
    if (!res.ok) {
      return { ok: false, list: [], error: `Failed to load backups (${res.status})` };
    }
    const list = (await res.json()) as SiteNginxBackup[];
    return { ok: true, list: Array.isArray(list) ? list : [] };
  } catch (e) {
    return { ok: false, list: [], error: e instanceof Error ? e.message : 'Request failed' };
  }
}

export async function loadPhpIniBackupContent(v: string, name: string): Promise<string> {
  const res = await apiFetch(phpConfigUrl(v, 'backups/' + encodeURIComponent(name)));
  if (!res.ok) throw new Error(`Failed to load backup (${res.status})`);
  return await res.text();
}

export async function resetPhpIni(v: string): Promise<ResetNginxResult> {
  try {
    const res = await apiFetch(phpConfigUrl(v, 'reset'), { method: 'POST' });
    const data = await decodeJSONResult<{ ok?: boolean; error?: string }>(res);
    return { ok: Boolean(data.ok), error: data.error };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'Request failed' };
  }
}

export async function restorePhpIni(v: string, name: string = ''): Promise<RestoreNginxResult> {
  try {
    const res = await apiFetch(phpConfigUrl(v, 'restore'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name })
    });
    const data = await decodeJSONResult<{ ok?: boolean; error?: string; restored?: string; content?: string }>(res);
    return { ok: Boolean(data.ok), error: data.error, restored: data.restored, content: data.content };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'Request failed' };
  }
}
