import { writable, derived, get } from 'svelte/store';
import { apiJson, apiFetch } from '$lib/api';
import { wsMessage } from '$lib/ws';

export interface FrameworkWorker {
  name: string;
  label?: string;
  running?: boolean;
  failing?: boolean;
}

export interface Site {
  name?: string;
  domain: string;
  domains?: string[];
  conflicting_domains?: Array<{ domain: string; owned_by?: string }>;
  path?: string;
  branch?: string;
  php_version?: string;
  uses_php?: boolean;
  node_version?: string;
  runtime?: string;
  runtime_worker?: boolean;
  tls?: boolean;
  fpm_running?: boolean;
  framework?: string;
  framework_label?: string;
  has_favicon?: boolean;
  has_env?: boolean;
  paused?: boolean;
  services?: string[];
  custom_container?: boolean;
  container_image?: string;
  container_port?: number;
  worktrees?: Array<{
    branch?: string;
    domain?: string;
    path?: string;
    php_version?: string;
    node_version?: string;
    php_version_override?: boolean;
    node_version_override?: boolean;
    framework_version?: string;
    framework_label?: string;
    db_isolated?: boolean;
    db_database?: string;
    lan_port?: number;
    lan_share_url?: string;
    framework_workers?: FrameworkWorker[];
  }>;
  has_queue_worker?: boolean;
  has_schedule_worker?: boolean;
  has_horizon?: boolean;
  has_reverb?: boolean;
  has_app_logs?: boolean;
  is_laravel?: boolean;
  queue_running?: boolean;
  queue_failing?: boolean;
  horizon_running?: boolean;
  horizon_failing?: boolean;
  horizon_reload?: boolean;
  horizon_reload_ready?: boolean;
  stripe_running?: boolean;
  stripe_secret_set?: boolean;
  schedule_running?: boolean;
  schedule_failing?: boolean;
  reverb_running?: boolean;
  reverb_failing?: boolean;
  lan_port?: number;
  lan_share_url?: string;
  framework_workers?: FrameworkWorker[];
  latest_log_time?: string;
  [k: string]: unknown;
}

export const sites = writable<Site[]>([]);
export const sitesLoaded = writable<boolean>(false);

export async function loadSites() {
  try {
    const list = await apiJson<Site[]>('/api/sites');
    sites.set(Array.isArray(list) ? list : []);
    sitesLoaded.set(true);
  } catch {
    /* keep previous */
  }
}

export function applySites(data: unknown) {
  if (!Array.isArray(data)) return;
  sites.set(data as Site[]);
  sitesLoaded.set(true);
}

wsMessage.subscribe((msg) => {
  if (msg?.sites) applySites(msg.sites);
});

export const sitesByPhp = derived(sites, ($s) => {
  const counts = new Map<string, number>();
  for (const site of $s) {
    if (site.php_version) counts.set(site.php_version, (counts.get(site.php_version) ?? 0) + 1);
  }
  return counts;
});

export const sitesByNode = derived(sites, ($s) => {
  const counts = new Map<string, number>();
  for (const site of $s) {
    if (site.node_version) counts.set(site.node_version, (counts.get(site.node_version) ?? 0) + 1);
  }
  return counts;
});

export function phpSiteCount(v: string): number {
  return get(sitesByPhp).get(v) ?? 0;
}
export function nodeSiteCount(v: string): number {
  return get(sitesByNode).get(v) ?? 0;
}

export function findSite(domain: string): Site | undefined {
  return get(sites).find((s) => s.domain === domain);
}

export function siteWorkerFailing(s: Site): boolean {
  return Boolean(
    s.queue_failing ||
      s.horizon_failing ||
      s.schedule_failing ||
      s.reverb_failing ||
      (s.framework_workers || []).some((w) => w.failing)
  );
}

export function openSiteInBrowser(s: Site, branch: string = '') {
  const target = activeWorktreeDomain(s, branch);
  const useTLS = Boolean(s.tls);
  const url = (useTLS ? 'https://' : 'http://') + target;
  window.open(url, '_blank', 'noopener');
}

export function activeWorktreeDomain(s: Site, branch: string): string {
  if (!branch) return s.domain;
  const wt = (s.worktrees || []).find((w) => w.branch === branch);
  return wt?.domain || s.domain;
}

async function postAction(path: string): Promise<{ ok: boolean; error?: string }> {
  try {
    const res = await apiFetch(path, { method: 'POST' });
    const data = (await res.json()) as { ok?: boolean; error?: string };
    return { ok: Boolean(data.ok), error: data.error };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'Request failed' };
  }
}

function site(path: string, action: string): string {
  return `/api/sites/${encodeURIComponent(path)}/${action}`;
}

function envQS(branch: string, file?: string): string {
  const params = new URLSearchParams();
  if (branch) params.set('branch', branch);
  if (file && file !== '.env') params.set('file', file);
  const s = params.toString();
  return s ? '?' + s : '';
}

export async function loadSiteEnvFiles(domain: string, branch: string = ''): Promise<string[]> {
  try {
    const res = await apiFetch(site(domain, 'env') + '/files' + envQS(branch));
    if (!res.ok) return ['.env'];
    const list = (await res.json()) as string[];
    return list.length > 0 ? list : ['.env'];
  } catch {
    return ['.env'];
  }
}

export async function loadSiteEnv(domain: string, branch: string = '', file: string = '.env'): Promise<string> {
  const res = await apiFetch(site(domain, 'env') + envQS(branch, file));
  if (!res.ok) throw new Error(`Failed to load ${file} (${res.status})`);
  return await res.text();
}

export interface SaveEnvResult {
  ok: boolean;
  error?: string;
  backupPath?: string;
}

export interface SiteEnvBackup {
  name: string;
  mtime_unix: number;
}

export async function loadSiteEnvBackups(
  domain: string,
  branch: string = '',
  file: string = '.env'
): Promise<SiteEnvBackup[]> {
  try {
    const res = await apiFetch(site(domain, 'env') + '/backups' + envQS(branch, file));
    if (!res.ok) return [];
    return (await res.json()) as SiteEnvBackup[];
  } catch {
    return [];
  }
}

export async function loadSiteEnvBackupContent(
  domain: string,
  name: string,
  branch: string = '',
  file: string = '.env'
): Promise<string> {
  const res = await apiFetch(site(domain, 'env') + '/backups/' + encodeURIComponent(name) + envQS(branch, file));
  if (!res.ok) throw new Error(`Failed to load backup (${res.status})`);
  return await res.text();
}

export interface RestoreEnvResult {
  ok: boolean;
  error?: string;
  restored?: string;
  content?: string;
}

export async function restoreSiteEnv(
  domain: string,
  branch: string = '',
  file: string = '.env',
  name: string = ''
): Promise<RestoreEnvResult> {
  try {
    const res = await apiFetch(site(domain, 'env') + '/restore' + envQS(branch, file), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name })
    });
    const data = (await res.json()) as { ok?: boolean; error?: string; restored?: string; content?: string };
    return { ok: Boolean(data.ok), error: data.error, restored: data.restored, content: data.content };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'Request failed' };
  }
}

export async function saveSiteEnv(
  domain: string,
  branch: string,
  content: string,
  backup: boolean,
  file: string = '.env'
): Promise<SaveEnvResult> {
  try {
    const res = await apiFetch(site(domain, 'env') + envQS(branch, file), {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content, backup })
    });
    const data = (await res.json()) as { ok?: boolean; error?: string; backup_path?: string };
    return { ok: Boolean(data.ok), error: data.error, backupPath: data.backup_path };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'Request failed' };
  }
}

export interface SiteNginx {
  path: string;
  content: string;
  exists: boolean;
}

export interface SiteNginxBackup {
  name: string;
  mtime_unix: number;
}

export interface SaveNginxResult {
  ok: boolean;
  error?: string;
  backupName?: string;
  validationOutput?: string;
  /** Canonical content after save; populated even on reload-failure so the
   *  client can refresh its `original` baseline (the bytes are on disk
   *  regardless of whether the runtime reload step succeeded). */
  content?: string;
  exists?: boolean;
}

export interface RestoreNginxResult {
  ok: boolean;
  error?: string;
  restored?: string;
  content?: string;
}

/** loadSiteNginxBackups returns either a list or an explicit failure.
 *  The earlier shape collapsed transport errors and 500s into [], so the
 *  UI could not tell "no backups exist" from "the server failed to read
 *  the backup directory" — the Restore button silently disappeared in
 *  both cases. Callers now have to handle the error branch explicitly. */
export interface LoadNginxBackupsResult {
  ok: boolean;
  list: SiteNginxBackup[];
  error?: string;
}

export async function getSiteNginx(domain: string): Promise<SiteNginx> {
  return apiJson<SiteNginx>(site(domain, 'nginx'));
}

export async function saveSiteNginx(
  domain: string,
  content: string,
  backup: boolean = false
): Promise<SaveNginxResult> {
  try {
    const res = await apiFetch(site(domain, 'nginx'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content, backup })
    });
    const data = (await res.json()) as {
      ok?: boolean;
      error?: string;
      backup_name?: string;
      validation_output?: string;
      content?: string;
      exists?: boolean;
    };
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

export async function loadSiteNginxBackups(domain: string): Promise<LoadNginxBackupsResult> {
  try {
    const res = await apiFetch(site(domain, 'nginx') + '/backups');
    if (!res.ok) {
      return { ok: false, list: [], error: `Failed to load backups (${res.status})` };
    }
    const list = (await res.json()) as SiteNginxBackup[];
    return { ok: true, list: Array.isArray(list) ? list : [] };
  } catch (e) {
    return { ok: false, list: [], error: e instanceof Error ? e.message : 'Request failed' };
  }
}

export async function loadSiteNginxBackupContent(domain: string, name: string): Promise<string> {
  const res = await apiFetch(site(domain, 'nginx') + '/backups/' + encodeURIComponent(name));
  if (!res.ok) throw new Error(`Failed to load backup (${res.status})`);
  return await res.text();
}

export interface ResetNginxResult {
  ok: boolean;
  error?: string;
}

export async function resetSiteNginx(domain: string): Promise<ResetNginxResult> {
  try {
    const res = await apiFetch(site(domain, 'nginx') + '/reset', { method: 'POST' });
    const data = (await res.json()) as { ok?: boolean; error?: string };
    return { ok: Boolean(data.ok), error: data.error };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'Request failed' };
  }
}

export async function restoreSiteNginx(
  domain: string,
  name: string = ''
): Promise<RestoreNginxResult> {
  try {
    const res = await apiFetch(site(domain, 'nginx') + '/restore', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name })
    });
    const data = (await res.json()) as { ok?: boolean; error?: string; restored?: string; content?: string };
    return { ok: Boolean(data.ok), error: data.error, restored: data.restored, content: data.content };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'Request failed' };
  }
}

export const restartSite = (d: string) => postAction(site(d, 'restart'));
export const pauseSite = (d: string) => postAction(site(d, 'pause'));
export const resumeSite = (d: string) => postAction(site(d, 'unpause'));
export const unlinkSite = (d: string) => postAction(site(d, 'unlink'));
export const openTerminal = (d: string, branch: string = '') =>
  postAction(site(d, 'terminal') + (branch ? `?branch=${encodeURIComponent(branch)}` : ''));

export function setWorktreeDBIsolated(
  d: string,
  branch: string,
  isolated: boolean,
  source: string = ''
) {
  const params = new URLSearchParams({ branch, isolated: String(isolated) });
  if (isolated && source) params.set('source', source);
  return postAction(site(d, 'db:isolate') + '?' + params.toString());
}

export const toggleTLS = (s: Site) => postAction(site(s.domain, s.tls ? 'unsecure' : 'secure'));
export const toggleLANShare = (s: Site, branch: string = '') => {
  const wt = branch ? (s.worktrees || []).find((w) => w.branch === branch) : undefined;
  const isOn = branch ? Boolean(wt?.lan_port) : Boolean(s.lan_port);
  const action = isOn ? 'lan:unshare' : 'lan:share';
  const qs = branch ? `?branch=${encodeURIComponent(branch)}` : '';
  return postAction(site(s.domain, action) + qs);
};
export const toggleQueue = (s: Site) =>
  postAction(site(s.domain, s.queue_running ? 'queue:stop' : 'queue:start'));
export const toggleHorizon = (s: Site) =>
  postAction(site(s.domain, s.horizon_running ? 'horizon:stop' : 'horizon:start'));
export const setHorizonReload = (s: Site, enabled: boolean) =>
  postAction(site(s.domain, 'horizon:reload') + `?enabled=${enabled ? 'true' : 'false'}`);
export const installHorizonReloadWatcher = (s: Site) =>
  postAction(site(s.domain, 'horizon:install-watcher'));
export const toggleSchedule = (s: Site) =>
  postAction(site(s.domain, s.schedule_running ? 'schedule:stop' : 'schedule:start'));
export const toggleReverb = (s: Site) =>
  postAction(site(s.domain, s.reverb_running ? 'reverb:stop' : 'reverb:start'));
export const toggleStripe = (s: Site) =>
  postAction(site(s.domain, s.stripe_running ? 'stripe:stop' : 'stripe:start'));
export const toggleWorker = (s: Site, w: FrameworkWorker, branch: string = '') =>
  postAction(
    site(s.domain, 'worker:' + w.name + (w.running ? ':stop' : ':start')) +
      (branch ? `?branch=${encodeURIComponent(branch)}` : '')
  );

export type TinkerResponse = {
  ok: boolean;
  stdout: string;
  stderr: string;
  exit_code: number;
  duration_ms: number;
  mode: 'tinker' | 'php';
  error?: string;
};

export type TinkerSymbols = {
  models: string[];
  classes: string[];
  functions: string[];
};

export type TinkerLintDiagnostic = {
  line: number;
  column: number;
  message: string;
  severity: 'error' | 'warning';
};

export type TinkerLintResponse = {
  ok: boolean;
  diagnostics: TinkerLintDiagnostic[];
  error?: string;
};

function tinkerURL(domain: string, action: string, branch: string): string {
  const base = site(domain, action);
  return branch ? `${base}?branch=${encodeURIComponent(branch)}` : base;
}

export async function lintTinker(
  domain: string,
  code: string,
  branch: string = ''
): Promise<TinkerLintResponse> {
  try {
    const res = await apiFetch(tinkerURL(domain, 'tinker:lint', branch), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code })
    });
    return (await res.json()) as TinkerLintResponse;
  } catch (e) {
    return {
      ok: false,
      diagnostics: [],
      error: e instanceof Error ? e.message : 'Request failed'
    };
  }
}

// Symbol responses are stable for the lifetime of the tab session
// (project classes, composer helpers, PHP internals don't change while
// the user is at the editor). Cache per-domain+branch so quick tab
// switches don't re-trigger the ~80 ms PHP exec on the backend, but
// switching worktree pulls the worktree's own symbol set.
const tinkerSymbolsCache = new Map<string, Promise<TinkerSymbols>>();

export async function loadTinkerSymbols(
  domain: string,
  branch: string = ''
): Promise<TinkerSymbols> {
  const key = `${domain}@${branch}`;
  const cached = tinkerSymbolsCache.get(key);
  if (cached) return cached;
  const p = (async () => {
    try {
      const res = await apiFetch(tinkerURL(domain, 'tinker:symbols', branch), { method: 'POST' });
      return (await res.json()) as TinkerSymbols;
    } catch {
      return { models: [], classes: [], functions: [] };
    }
  })();
  tinkerSymbolsCache.set(key, p);
  return p;
}

export async function runTinker(
  domain: string,
  code: string,
  branch: string = ''
): Promise<TinkerResponse> {
  try {
    const res = await apiFetch(tinkerURL(domain, 'tinker', branch), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code })
    });
    const data = (await res.json()) as TinkerResponse;
    return data;
  } catch (e) {
    return {
      ok: false,
      stdout: '',
      stderr: '',
      exit_code: -1,
      duration_ms: 0,
      mode: 'php',
      error: e instanceof Error ? e.message : 'Request failed'
    };
  }
}

export async function setSiteVersion(
  s: Site,
  type: 'php' | 'node',
  version: string,
  branch: string = ''
) {
  try {
    const params = new URLSearchParams({ version });
    if (branch) params.set('branch', branch);
    const res = await apiFetch(site(s.domain, type) + '?' + params.toString(), {
      method: 'POST'
    });
    const data = (await res.json()) as { ok?: boolean; error?: string };
    return { ok: Boolean(data.ok), error: data.error };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'Request failed' };
  }
}

export function fpmContainer(s: Site): string {
  if (s.custom_container) return 'lerd-custom-' + (s.name || s.domain);
  if (s.runtime === 'frankenphp') return 'lerd-fp-' + (s.name || s.domain);
  if (!s.php_version) return '';
  return 'lerd-php' + s.php_version.replace('.', '') + '-fpm';
}

export function fpmTabLabel(s: Site): string {
  if (s.custom_container) return 'Container';
  if (s.runtime === 'frankenphp') return 'FrankenPHP';
  return 'PHP-FPM';
}
