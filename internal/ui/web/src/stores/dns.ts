import { apiJson, apiFetch, decodeJSONResult } from '$lib/api';

export interface DnsUpstreamSettings {
  // upstream holds the user-pinned upstream DNS servers. Empty means lerd
  // auto-detects them from the system resolver.
  upstream: string[];
  // detected is what auto-detection currently sees, shown as a hint so the
  // user knows what would be used if they leave the override empty.
  detected: string[];
}

interface SettingsResponse {
  dns_upstream?: string[];
  dns_upstream_detected?: string[];
}

function isIPv4(s: string): boolean {
  const parts = s.split('.');
  if (parts.length !== 4) return false;
  return parts.every((p) => /^\d{1,3}$/.test(p) && Number(p) <= 255);
}

function isIPv6(s: string): boolean {
  if (s === '' || s.includes(':::') || !/^[0-9a-fA-F:]+$/.test(s)) return false;
  const compressed = s.includes('::');
  if (compressed && (s.match(/::/g) ?? []).length > 1) return false;
  const groups = s.split(':');
  for (const g of groups) {
    if (g !== '' && !/^[0-9a-fA-F]{1,4}$/.test(g)) return false;
  }
  const filled = groups.filter((g) => g !== '').length;
  return compressed ? filled <= 7 : groups.length === 8 && filled === 8;
}

// isValidUpstream accepts an IPv4 or IPv6 address with an optional dnsmasq-style
// "#port" suffix (1-65535). Format only; the server is the final authority and
// also rejects loopback/unspecified addresses.
export function isValidUpstream(entry: string): boolean {
  const t = entry.trim();
  if (t === '') return false;
  const hash = t.indexOf('#');
  let ip = t;
  if (hash >= 0) {
    ip = t.slice(0, hash);
    const port = t.slice(hash + 1);
    if (!/^\d+$/.test(port)) return false;
    const n = Number(port);
    if (n < 1 || n > 65535) return false;
  }
  return isIPv4(ip) || isIPv6(ip);
}

export async function loadDnsUpstream(): Promise<DnsUpstreamSettings> {
  const res = await apiJson<SettingsResponse>('/api/settings');
  return {
    upstream: res.dns_upstream ?? [],
    detected: res.dns_upstream_detected ?? []
  };
}

export async function saveDnsUpstream(
  upstream: string[]
): Promise<{ ok: boolean; error?: string; upstream?: string[] }> {
  try {
    const res = await apiFetch('/api/settings/dns-upstream', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ upstream })
    });
    const data = await decodeJSONResult<{ ok?: boolean; error?: string; upstream?: string[] }>(res);
    return { ok: Boolean(data.ok), error: data.error, upstream: data.upstream };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'Request failed' };
  }
}
