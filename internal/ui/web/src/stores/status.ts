import { writable, derived, get } from 'svelte/store';
import { apiJson } from '$lib/api';
import { wsMessage } from '$lib/ws';
import { version } from './version';

export interface PHPStatus {
  version: string;
  running: boolean;
  xdebug_enabled: boolean;
  xdebug_mode?: string;
}

export interface StatusResponse {
  dns: { ok: boolean; tld: string };
  nginx: { running: boolean };
  php_fpms: PHPStatus[];
  php_default: string;
  node_default: string;
  node_managed_by_lerd: boolean;
  watcher_running: boolean;
}

const empty: StatusResponse = {
  dns: { ok: false, tld: 'test' },
  nginx: { running: false },
  php_fpms: [],
  php_default: '',
  node_default: '',
  node_managed_by_lerd: true,
  watcher_running: false
};

export const status = writable<StatusResponse>(empty);
export const statusLoaded = writable<boolean>(false);

export async function loadStatus() {
  try {
    const res = await apiJson<StatusResponse>('/api/status');
    status.set({ ...empty, ...res });
    statusLoaded.set(true);
  } catch {
    /* keep previous */
  }
}

export function applyStatus(data: unknown) {
  if (!data || typeof data !== 'object') return;
  status.set({ ...empty, ...(data as StatusResponse) });
  statusLoaded.set(true);
}

wsMessage.subscribe((msg) => {
  if (msg?.status) applyStatus(msg.status);
});

export type LerdStatusColor = 'green' | 'yellow' | 'red' | 'gray';

export const lerdStatusColor = derived([status, statusLoaded, version], ([$s, $loaded, $v]): LerdStatusColor => {
  if (!$loaded) return 'gray';
  const coreOk = $s.dns.ok && $s.nginx.running && $s.watcher_running;
  if (!coreOk) return 'red';
  if ($v.hasUpdate) return 'yellow';
  return 'green';
});

export const allCoreRunning = derived(status, ($s): boolean => {
  return Boolean(
    $s.dns.ok &&
      $s.nginx.running &&
      ($s.php_fpms || []).every((f) => f.running)
  );
});

export function fpmRunning(v: string): boolean {
  const fpm = get(status).php_fpms.find((f) => f.version === v);
  return Boolean(fpm?.running);
}
