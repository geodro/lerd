import { writable } from 'svelte/store';
import { apiJson, apiFetch, decodeJSONResult } from '$lib/api';
import { readSSE } from '$lib/sse';
import type { SiteNginxBackup, LoadNginxBackupsResult, ResetNginxResult, SaveNginxResult, RestoreNginxResult } from './sites';

export const phpVersions = writable<string[]>([]);

export interface PhpOption {
  value: string;
  disabled?: boolean;
  description?: string;
}

// cmpVersion compares two "major.minor" strings numerically.
function cmpVersion(a: string, b: string): number {
  const [aMaj, aMin] = a.split('.').map((n) => parseInt(n, 10) || 0);
  const [bMaj, bMin] = b.split('.').map((n) => parseInt(n, 10) || 0);
  return aMaj !== bMaj ? aMaj - bMaj : aMin - bMin;
}

// outOfFrameworkRange reports whether a PHP version falls outside the
// framework's [min, max] range. An empty bound means unconstrained on that side.
function outOfFrameworkRange(v: string, min?: string, max?: string): boolean {
  if (min && cmpVersion(v, min) < 0) return true;
  if (max && cmpVersion(v, max) > 0) return true;
  return false;
}

// phpOptionsForSite returns the options to offer in a site's PHP dropdown.
// FrankenPHP sites are limited to the versions dunglas/frankenphp publishes an
// image for, intersected with what's installed, plus the site's current version
// so the control never renders blank. Other runtimes offer every installed one.
// When the framework declares a PHP range (min/max), versions outside it are
// kept in the list but disabled, so the constraint is visible rather than
// silently hidden. The site's current version is never disabled. min/max are
// empty when the framework version was guessed, leaving every version enabled.
export function phpOptionsForSite(
  runtime: string | undefined,
  installed: string[],
  frankenphpVersions: string[],
  current: string,
  min?: string,
  max?: string
): PhpOption[] {
  const base =
    runtime === 'frankenphp'
      ? frankenphpVersions.filter((v) => installed.includes(v) || v === current)
      : installed;
  return base.map((v) => {
    const disabled = v !== current && outOfFrameworkRange(v, min, max);
    return disabled
      ? { value: v, disabled: true, description: `needs PHP ${min || '*'} to ${max || '*'}` }
      : { value: v };
  });
}

export async function loadPhpVersions() {
  try {
    const list = await apiJson<string[]>('/api/php-versions');
    phpVersions.set(Array.isArray(list) ? list : []);
  } catch {
    /* keep previous */
  }
}

// installablePhpVersions returns the supported PHP versions not yet installed,
// so the add-version modal can offer them in a dropdown. Throws on request
// failure so the caller can distinguish an error from an empty (all-installed)
// list rather than silently showing "all installed".
export async function installablePhpVersions(): Promise<string[]> {
  const list = await apiJson<string[]>('/api/php-installable');
  return Array.isArray(list) ? list : [];
}

export interface PhpInstallEvent {
  line?: string;
  done?: boolean;
  ok?: boolean;
  version?: string;
  error?: string;
}

// streamPhpInstall POSTs to the SSE endpoint and invokes onEvent for each build
// log line and the final done payload. Mirrors streamWorktreeAdd. Pass a signal
// to abort the client read when the modal closes; the server build continues and
// reports its result via a push notification.
export async function streamPhpInstall(
  version: string,
  onEvent: (e: PhpInstallEvent) => void,
  signal?: AbortSignal
): Promise<void> {
  const res = await apiFetch('/api/php-versions/install?version=' + encodeURIComponent(version), {
    method: 'POST',
    signal
  });
  await readSSE(res, (event, data) => {
    if (event === 'done') {
      try {
        const r = JSON.parse(data) as { ok?: boolean; version?: string; error?: string };
        onEvent({ done: true, ok: Boolean(r.ok), version: r.version, error: r.error });
      } catch {
        onEvent({ done: true, ok: false, error: 'bad done payload' });
      }
    } else {
      onEvent({ line: data });
    }
  });
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
