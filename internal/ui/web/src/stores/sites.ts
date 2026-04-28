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
  node_version?: string;
  runtime?: string;
  runtime_worker?: boolean;
  tls?: boolean;
  fpm_running?: boolean;
  framework?: string;
  framework_label?: string;
  has_favicon?: boolean;
  paused?: boolean;
  services?: string[];
  custom_container?: boolean;
  container_image?: string;
  container_port?: number;
  worktrees?: Array<{ branch?: string; domain?: string; path?: string }>;
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

export function openSiteInBrowser(s: Site) {
  const url = (s.tls ? 'https://' : 'http://') + s.domain;
  window.open(url, '_blank', 'noopener');
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

export const restartSite = (d: string) => postAction(site(d, 'restart'));
export const pauseSite = (d: string) => postAction(site(d, 'pause'));
export const resumeSite = (d: string) => postAction(site(d, 'unpause'));
export const unlinkSite = (d: string) => postAction(site(d, 'unlink'));
export const openTerminal = (d: string) => postAction(site(d, 'terminal'));

export const toggleTLS = (s: Site) => postAction(site(s.domain, s.tls ? 'unsecure' : 'secure'));
export const toggleLANShare = (s: Site) => postAction(site(s.domain, s.lan_port ? 'lan:unshare' : 'lan:share'));
export const toggleQueue = (s: Site) =>
  postAction(site(s.domain, s.queue_running ? 'queue:stop' : 'queue:start'));
export const toggleHorizon = (s: Site) =>
  postAction(site(s.domain, s.horizon_running ? 'horizon:stop' : 'horizon:start'));
export const toggleSchedule = (s: Site) =>
  postAction(site(s.domain, s.schedule_running ? 'schedule:stop' : 'schedule:start'));
export const toggleReverb = (s: Site) =>
  postAction(site(s.domain, s.reverb_running ? 'reverb:stop' : 'reverb:start'));
export const toggleStripe = (s: Site) =>
  postAction(site(s.domain, s.stripe_running ? 'stripe:stop' : 'stripe:start'));
export const toggleWorker = (s: Site, w: FrameworkWorker) =>
  postAction(site(s.domain, 'worker:' + w.name + (w.running ? ':stop' : ':start')));

export async function setSiteVersion(s: Site, type: 'php' | 'node', version: string) {
  try {
    const res = await apiFetch(site(s.domain, type) + '?version=' + encodeURIComponent(version), {
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
