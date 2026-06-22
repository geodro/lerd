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
  app_name?: string;
  domain: string;
  domains?: string[];
  conflicting_domains?: Array<{ domain: string; owned_by?: string }>;
  path?: string;
  branch?: string;
  php_version?: string;
  php_min?: string;
  php_max?: string;
  uses_php?: boolean;
  node_version?: string;
  js_runtime?: string;
  runtime?: string;
  runtime_worker?: boolean;
  tls?: boolean;
  fpm_running?: boolean;
  framework?: string;
  framework_label?: string;
  has_favicon?: boolean;
  has_env?: boolean;
  paused?: boolean;
  pinned?: boolean;
  idle_suspended?: boolean;
  idle?: boolean;
  idle_suspended_workers?: string[];
  services?: string[];
  custom_container?: boolean;
  container_image?: string;
  container_port?: number;
  host_proxy?: boolean;
  host_port?: number;
  host_has_dev_server?: boolean;
  group?: string;
  group_subdomain?: string;
  group_main_domain?: string;
  group_shared_db?: boolean;
  multi_tenant?: boolean;
  worktrees?: Array<{
    branch?: string;
    domain?: string;
    path?: string;
    php_version?: string;
    php_min?: string;
    php_max?: string;
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
    idle_suspended_workers?: string[];
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
  octane_reload?: boolean;
  octane_reload_ready?: boolean;
  stripe_running?: boolean;
  stripe_secret_set?: boolean;
  stripe_webhook_path?: string;
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

export function isGroupSecondary(s: Site): boolean {
  return Boolean(s.group) && Boolean(s.group_subdomain);
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

export type WorkerDotColor = 'amber' | 'violet' | 'emerald' | 'sky' | 'indigo';

// Colors for each running worker, used as little status dots in list rows and
// dashboard tiles. Kept here so the row and tile views never drift apart.
export function runningWorkerColors(s: Site): WorkerDotColor[] {
  const dots: WorkerDotColor[] = [];
  if (s.queue_running) dots.push('amber');
  if (s.horizon_running) dots.push('amber');
  if (s.stripe_running) dots.push('violet');
  if (s.schedule_running) dots.push('emerald');
  if (s.reverb_running) dots.push('sky');
  for (const w of s.framework_workers || []) if (w.running) dots.push('indigo');
  return dots;
}

function workerColorByName(name: string): WorkerDotColor {
  if (name === 'queue' || name === 'horizon') return 'amber';
  if (name === 'stripe') return 'violet';
  if (name === 'schedule') return 'emerald';
  if (name === 'reverb') return 'sky';
  return 'indigo';
}

// idleWorkerColors returns the dot colors to show while a site sleeps: the
// workers the engine suspended (so they keep their dots, dimmed, instead of
// disappearing), falling back to whatever is still running during the brief
// suspend transition.
export function idleWorkerColors(s: Site): WorkerDotColor[] {
  const dots = (s.idle_suspended_workers || []).map(workerColorByName);
  return dots.length > 0 ? dots : runningWorkerColors(s);
}

// siteHasWorkers reports whether a site has any background worker at all (queue,
// schedule, horizon, reverb, a stripe listener, or a framework worker). Sites
// with none never sleep, so the dashboard must not show them the idle moon.
export function siteHasWorkers(s: Site): boolean {
  return Boolean(
    s.has_queue_worker ||
      s.has_schedule_worker ||
      s.has_horizon ||
      s.has_reverb ||
      s.stripe_running ||
      s.stripe_secret_set ||
      (s.framework_workers && s.framework_workers.length > 0) ||
      (s.idle_suspended_workers && s.idle_suspended_workers.length > 0)
  );
}

export function openSiteInBrowser(s: Site, branch: string = '') {
  const target = activeWorktreeDomain(s, branch);
  const useTLS = Boolean(s.tls);
  const url = (useTLS ? 'https://' : 'http://') + target;
  // Opening the site loads it, which wakes it via the activity feed. Clear the
  // idle marker optimistically so the sleep indicator drops immediately rather
  // than lingering until the next poll confirms the activity.
  sites.update((list) =>
    list.map((x) => (x.name === s.name ? { ...x, idle: false, idle_suspended: false } : x))
  );
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

export async function reorderSites(order: string[]): Promise<{ ok: boolean; error?: string }> {
  try {
    const res = await apiFetch('/api/sites/reorder', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ order })
    });
    const data = (await res.json()) as { ok?: boolean; error?: string };
    return { ok: Boolean(data.ok), error: data.error };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'Request failed' };
  }
}

export const restartSite = (d: string) => postAction(site(d, 'restart'));
export const pauseSite = (d: string) => postAction(site(d, 'pause'));
export const resumeSite = (d: string) => postAction(site(d, 'unpause'));
export const pinSite = (d: string) => postAction(site(d, 'pin'));
export const unpinSite = (d: string) => postAction(site(d, 'unpin'));
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
export const setOctaneReload = (s: Site, enabled: boolean) =>
  postAction(site(s.domain, 'octane:reload') + `?enabled=${enabled ? 'true' : 'false'}`);
export const installOctaneReloadWatcher = (s: Site) =>
  postAction(site(s.domain, 'octane:install-watcher'));
export const toggleSchedule = (s: Site) =>
  postAction(site(s.domain, s.schedule_running ? 'schedule:stop' : 'schedule:start'));
export const toggleReverb = (s: Site) =>
  postAction(site(s.domain, s.reverb_running ? 'reverb:stop' : 'reverb:start'));
export const toggleStripe = (s: Site) =>
  postAction(site(s.domain, s.stripe_running ? 'stripe:stop' : 'stripe:start'));
export const setStripeConfig = (s: Site, path: string) =>
  postAction(site(s.domain, 'stripe:config') + '?path=' + encodeURIComponent(path));
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

function tinkerURL(domain: string, action: string, branch: string): string {
  const base = site(domain, action);
  return branch ? `${base}?branch=${encodeURIComponent(branch)}` : base;
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
  if (s.runtime === 'fpm-custom') return 'lerd-cfpm-' + (s.name || s.domain);
  if (!s.php_version) return '';
  return 'lerd-php' + s.php_version.replace('.', '') + '-fpm';
}

export function fpmTabLabel(s: Site): string {
  if (s.custom_container) return 'Container';
  if (s.runtime === 'frankenphp') return 'FrankenPHP';
  if (s.runtime === 'fpm-custom') return 'Custom FPM';
  return 'PHP-FPM';
}
