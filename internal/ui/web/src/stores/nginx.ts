import { apiJson, apiFetch, decodeJSONResult } from '$lib/api';
import type { SiteNginxBackup, LoadNginxBackupsResult, ResetNginxResult, SaveNginxResult, RestoreNginxResult } from './sites';

export interface NginxConfig {
  path: string;
  content: string;
  exists: boolean;
}

export async function getNginxConfig(): Promise<NginxConfig> {
  return apiJson<NginxConfig>('/api/nginx/config');
}

export async function saveNginxConfig(content: string, backup: boolean = false): Promise<SaveNginxResult> {
  try {
    const res = await apiFetch('/api/nginx/config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content, backup })
    });
    const data = await decodeJSONResult<{
      ok?: boolean;
      error?: string;
      backup_name?: string;
      validation_output?: string;
      content?: string;
      exists?: boolean;
    }>(res);
    return {
      ok: Boolean(data.ok),
      error: data.error,
      backupName: data.backup_name,
      validationOutput: data.validation_output,
      content: data.content,
      exists: data.exists
    };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'Request failed' };
  }
}

export async function loadNginxConfigBackups(): Promise<LoadNginxBackupsResult> {
  try {
    const res = await apiFetch('/api/nginx/backups');
    if (!res.ok) {
      return { ok: false, list: [], error: `Failed to load backups (${res.status})` };
    }
    const list = (await res.json()) as SiteNginxBackup[];
    return { ok: true, list: Array.isArray(list) ? list : [] };
  } catch (e) {
    return { ok: false, list: [], error: e instanceof Error ? e.message : 'Request failed' };
  }
}

export async function loadNginxConfigBackupContent(name: string): Promise<string> {
  const res = await apiFetch('/api/nginx/backups/' + encodeURIComponent(name));
  if (!res.ok) throw new Error(`Failed to load backup (${res.status})`);
  return await res.text();
}

export async function resetNginxConfig(): Promise<ResetNginxResult> {
  try {
    const res = await apiFetch('/api/nginx/reset', { method: 'POST' });
    const data = await decodeJSONResult<{ ok?: boolean; error?: string }>(res);
    return { ok: Boolean(data.ok), error: data.error };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'Request failed' };
  }
}

export async function restoreNginxConfig(name: string = ''): Promise<RestoreNginxResult> {
  try {
    const res = await apiFetch('/api/nginx/restore', {
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
