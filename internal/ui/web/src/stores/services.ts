import { writable, derived, get } from 'svelte/store';
import { apiJson, apiFetch } from '$lib/api';
import { wsMessage } from '$lib/ws';
import { sites } from './sites';

export interface Service {
  name: string;
  status: string;
  version?: string;
  env_vars?: Record<string, string>;
  dashboard?: string;
  dashboard_external?: boolean;
  connection_url?: string;
  custom?: boolean;
  is_default?: boolean;
  site_count: number;
  site_domains?: string[];
  pinned?: boolean;
  paused?: boolean;
  depends_on?: string[];
  queue_site?: string;
  stripe_listener_site?: string;
  schedule_worker_site?: string;
  reverb_site?: string;
  horizon_site?: string;
  worker_site?: string;
  worker_name?: string;
  worker_label?: string;
  worker_worktree?: string;
  worker_worktree_domain?: string;
  update_strategy?: string;
  update_available?: boolean;
  latest_version?: string;
  upgrade_version?: string;
  previous_version?: string;
  migration_supported?: boolean;
  can_rollback?: boolean;
  port_conflicts?: PortConflict[];
}

export interface PortConflict {
  port: string;
  label?: string;
}

export interface PhaseEvent {
  phase: string;
  image?: string;
  message?: string;
  dep?: string;
  state?: string;
  unit?: string;
  error?: string;
}

export interface UpdateProgress {
  phase: string;
  message: string;
  error?: boolean;
}

export const updateProgress = writable<Record<string, UpdateProgress>>({});

export const services = writable<Service[]>([]);
export const servicesLoaded = writable<boolean>(false);

export async function loadServices() {
  try {
    const list = await apiJson<Service[]>('/api/services');
    services.set(Array.isArray(list) ? list : []);
    servicesLoaded.set(true);
  } catch {
    /* keep previous */
  }
}

export function applyServices(data: unknown) {
  if (!Array.isArray(data)) return;
  services.set(data as Service[]);
  servicesLoaded.set(true);
}

wsMessage.subscribe((msg) => {
  if (msg?.services) applyServices(msg.services);
});

function isWorker(s: Service): boolean {
  return Boolean(
    s.queue_site ||
      s.horizon_site ||
      s.stripe_listener_site ||
      s.schedule_worker_site ||
      s.reverb_site ||
      s.worker_site
  );
}

export const coreServices = derived(services, ($s) => $s.filter((x) => !isWorker(x)));

export interface WorkerGroup {
  key: string;
  label: string;
  items: Service[];
}

export const workerGroups = derived(services, ($s): WorkerGroup[] => {
  // Horizon double-emits: the backend lists it under horizon_site AND the
  // generic worker_site lens. Drop the worker_site copy here.
  const workers = $s.filter(
    (x) => x.worker_site && !(x.worker_name === 'horizon' || x.name?.startsWith('horizon-'))
  );
  // Bucket store-defined workers (vite, etc.) by worker_name so each worker
  // gets its own group instead of all sharing a generic "Workers" header.
  const byName = new Map<string, { label: string; items: Service[] }>();
  for (const w of workers) {
    const key = w.worker_name || 'workers';
    const label = w.worker_label || w.worker_name || 'Workers';
    const bucket = byName.get(key) || { label, items: [] };
    bucket.items.push(w);
    byName.set(key, bucket);
  }
  const dynamic: WorkerGroup[] = [...byName.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, b]) => ({ key, label: b.label, items: b.items }));

  const groups: WorkerGroup[] = [
    { key: 'queue', label: 'Queues', items: $s.filter((x) => x.queue_site) },
    { key: 'horizon', label: 'Horizon', items: $s.filter((x) => x.horizon_site) },
    { key: 'schedule', label: 'Schedules', items: $s.filter((x) => x.schedule_worker_site) },
    { key: 'reverb', label: 'Reverb', items: $s.filter((x) => x.reverb_site) },
    { key: 'stripe', label: 'Stripe', items: $s.filter((x) => x.stripe_listener_site) },
    ...dynamic
  ];
  return groups.filter((g) => g.items.length > 0);
});

export type ServiceAction = 'start' | 'stop' | 'restart' | 'pin' | 'unpin' | 'remove';

export async function serviceAction(
  name: string,
  action: ServiceAction,
  opts: { removeData?: boolean } = {}
): Promise<boolean> {
  try {
    const params = new URLSearchParams();
    if (action === 'remove' && opts.removeData) params.set('removeData', 'true');
    const url =
      '/api/services/' +
      encodeURIComponent(name) +
      '/' +
      action +
      (params.toString() ? '?' + params.toString() : '');
    const res = await apiFetch(url, { method: 'POST' });
    if (res.ok) await loadServices();
    return res.ok;
  } catch {
    return false;
  }
}

export type UpdateAction = 'update' | 'migrate' | 'rollback' | 'reinstall';

function setProgress(name: string, p: UpdateProgress | null) {
  updateProgress.update((m) => {
    const next = { ...m };
    if (p === null) delete next[name];
    else next[name] = p;
    return next;
  });
}

function phaseLabel(phase: string): string {
  switch (phase) {
    case 'checking_registry':
      return 'Checking registry…';
    case 'pulling_image':
      return 'Pulling image…';
    case 'writing_quadlet':
      return 'Writing unit…';
    case 'restarting_unit':
      return 'Restarting…';
    case 'waiting_ready':
      return 'Waiting for ready…';
    case 'dumping_data':
      return 'Dumping data…';
    case 'restoring_data':
      return 'Restoring data…';
    case 'swapping_data_dir':
      return 'Swapping data dir…';
    case 'starting_deps':
      return 'Starting dependencies…';
    case 'reinstall_starting':
      return 'Reinstalling…';
    case 'stopping_unit':
      return 'Stopping…';
    case 'removing_container':
      return 'Removing container…';
    case 'removing_data':
      return 'Renaming data dir aside…';
    case 'removing_quadlet':
      return 'Removing unit…';
    case 'removing_config':
      return 'Removing config…';
    case 'regenerating_consumers':
      return 'Refreshing consumers…';
    case 'reprovisioning_sites':
      return 'Reprovisioning linked sites…';
    case 'reprovisioning_site':
      return 'Reprovisioning…';
    case 'reprovisioning_skipped':
      return 'Reprovisioning skipped';
    case 'starting_unit':
      return 'Starting…';
    case 'installing_config':
      return 'Writing config…';
    case 'done':
      return 'Done';
    default:
      return phase || 'Working…';
  }
}

export async function streamServiceAction(
  name: string,
  action: UpdateAction,
  opts: { tag?: string; resetData?: boolean } = {}
): Promise<{ ok: boolean; error?: string }> {
  const params = new URLSearchParams();
  if (opts.tag) params.set('tag', opts.tag);
  if (action === 'reinstall' && opts.resetData) params.set('resetData', 'true');
  const url = '/api/services/' + encodeURIComponent(name) + '/' + action + (params.toString() ? '?' + params.toString() : '');

  const initialPhase = action === 'reinstall' ? 'reinstall_starting' : 'checking_registry';
  setProgress(name, { phase: initialPhase, message: phaseLabel(initialPhase) });
  try {
    const res = await apiFetch(url, { method: 'POST' });
    if (!res.body) {
      setProgress(name, null);
      return { ok: false, error: 'streaming not supported' };
    }
    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buf = '';
    let finalError: string | undefined;
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      let nl: number;
      while ((nl = buf.indexOf('\n')) >= 0) {
        const line = buf.slice(0, nl).trim();
        buf = buf.slice(nl + 1);
        if (!line) continue;
        let evt: PhaseEvent;
        try {
          evt = JSON.parse(line) as PhaseEvent;
        } catch {
          continue;
        }
        if (evt.phase === 'error') {
          finalError = evt.error || 'failed';
          setProgress(name, { phase: 'error', message: finalError, error: true });
          continue;
        }
        if (evt.phase === 'done') continue;
        const message =
          evt.phase === 'pulling_image' && evt.image
            ? 'Pulling ' + evt.image
            : evt.message || phaseLabel(evt.phase);
        setProgress(name, { phase: evt.phase, message });
      }
    }
    if (finalError) {
      setTimeout(() => setProgress(name, null), 5000);
    } else {
      setProgress(name, null);
    }
    await loadServices();
    // Optimistic patch on success. The server's snapshot rebuild is async
    // (runs in the eventbus subscriber goroutine), so loadServices above
    // can race in and return the pre-rebuild snapshot — leaving the
    // "update available" badge and the latest_version pointer stuck on
    // the values we just resolved. Patch the relevant fields locally so
    // the badge disappears immediately; the next WS push will reconcile
    // with authoritative state (including the new version label).
    if (!finalError) {
      const tagApplied = opts.tag || '';
      services.update((list) =>
        list.map((s) => {
          if (s.name !== name) return s;
          if (action === 'update' || action === 'migrate') {
            return {
              ...s,
              update_available: false,
              latest_version: '',
              upgrade_version: action === 'migrate' ? '' : s.upgrade_version,
              version: tagApplied ? 'v' + tagApplied : s.version
            };
          }
          if (action === 'rollback') {
            // rollback target is the previous_version image; leave update_available
            // alone since the server may flip it back true on the next check.
            return { ...s, previous_version: '', can_rollback: false };
          }
          return s;
        })
      );
    }
    return { ok: !finalError, error: finalError };
  } catch (e) {
    setProgress(name, null);
    return { ok: false, error: e instanceof Error ? e.message : 'request failed' };
  }
}

export function findService(name: string): Service | undefined {
  return get(services).find((s) => s.name === name);
}

export function serviceLabel(name: string): string {
  const overrides: Record<string, string> = {
    phpmyadmin: 'phpMyAdmin',
    pgadmin: 'pgAdmin',
    mysql: 'MySQL',
    postgres: 'PostgreSQL',
    meilisearch: 'Meilisearch',
    mailpit: 'Mailpit',
    rustfs: 'RustFS',
    mongo: 'MongoDB',
    'mongo-express': 'Mongo Express',
    'stripe-mock': 'Stripe Mock',
    elasticsearch: 'Elasticsearch',
    elasticvue: 'Elasticvue',
    memcached: 'Memcached',
    rabbitmq: 'RabbitMQ'
  };
  if (overrides[name]) return overrides[name];
  // Versioned variants like "mysql-5-7"; show the family label.
  const m = name.match(/^([a-z][a-z0-9]*?)-(\d[\w-]*)$/);
  if (m && overrides[m[1]]) return overrides[m[1]];
  if (m) return capitalize(m[1]);
  return capitalize(name);
}

function capitalize(s: string): string {
  return s
    .split('-')
    .map((w) => (w.length ? w[0].toUpperCase() + w.slice(1) : w))
    .join(' ');
}

export function workerSiteName(s: Service): string {
  const base =
    s.queue_site ||
    s.horizon_site ||
    s.schedule_worker_site ||
    s.reverb_site ||
    s.stripe_listener_site ||
    s.worker_site ||
    s.name;
  return s.worker_worktree ? base + '/' + s.worker_worktree : base;
}

export function parentSiteDomain(s: Service): string | null {
  if (s.worker_worktree_domain) return s.worker_worktree_domain;
  const n =
    s.queue_site ||
    s.horizon_site ||
    s.schedule_worker_site ||
    s.reverb_site ||
    s.stripe_listener_site ||
    s.worker_site;
  if (!n) return null;
  // Use the actual registered domain from the sites store rather than
  // constructing <name>.test, which silently breaks for sites with custom
  // TLDs or non-default subdomains.
  const site = get(sites).find((x) => x.name === n);
  if (site && site.domain) return site.domain;
  return null;
}

export function detailLabel(s: Service): string {
  if (s.queue_site) return 'Queue worker';
  if (s.horizon_site) return 'Horizon';
  if (s.stripe_listener_site) return 'Stripe listener';
  if (s.schedule_worker_site) return 'Scheduler';
  if (s.reverb_site) return 'Reverb';
  if (s.worker_site && s.worker_name) return s.worker_name + ' worker';
  return serviceLabel(s.name);
}

export function isServiceWorker(s: Service): boolean {
  return isWorker(s);
}
