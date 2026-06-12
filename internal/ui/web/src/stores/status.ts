import { writable, derived } from 'svelte/store';
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
  dns: { ok: boolean; status?: 'ok' | 'degraded' | 'down'; vpn?: boolean; enabled: boolean; tld: string };
  nginx: { running: boolean };
  php_fpms: PHPStatus[];
  php_default: string;
  node_default: string;
  node_managed_by_lerd: boolean;
  bun_available: boolean;
  bun_version: string;
  using_system_bun: boolean;
  watcher_running: boolean;
  frankenphp_php_versions: string[];
}

const empty: StatusResponse = {
  dns: { ok: false, status: 'down', vpn: false, enabled: true, tld: 'test' },
  nginx: { running: false },
  php_fpms: [],
  php_default: '',
  node_default: '',
  node_managed_by_lerd: true,
  bun_available: false,
  bun_version: '',
  using_system_bun: false,
  watcher_running: false,
  frankenphp_php_versions: []
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

export type DnsState = 'ok' | 'degraded' | 'down';

// dnsState collapses the payload into a three-way health value. It tolerates
// older payloads without the `status` field by deriving it from `ok`, and
// treats lerd-managed DNS being disabled as healthy since the system
// resolver owns *.tld in that mode. "degraded" means lerd-dns answers fine
// but the system resolver isn't routing to it, typically a VPN client.
export function dnsState(s: StatusResponse): DnsState {
  if (s.dns.enabled === false) return 'ok';
  return s.dns.status ?? (s.dns.ok ? 'ok' : 'down');
}

export type LerdStatusColor = 'green' | 'yellow' | 'red' | 'gray';

export const lerdStatusColor = derived([status, statusLoaded, version], ([$s, $loaded, $v]): LerdStatusColor => {
  if (!$loaded) return 'gray';
  const dns = dnsState($s);
  if (dns === 'down' || !$s.nginx.running || !$s.watcher_running) return 'red';
  if (dns === 'degraded' || $v.hasUpdate) return 'yellow';
  return 'green';
});

export const allCoreRunning = derived(status, ($s): boolean => {
  return Boolean(
    dnsState($s) !== 'down' &&
      $s.nginx.running &&
      ($s.php_fpms || []).every((f) => f.running)
  );
});

