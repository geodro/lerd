export const apiBase =
  typeof location !== 'undefined' && location.hostname === 'lerd.localhost'
    ? 'http://localhost:7073'
    : '';

export function apiUrl(path: string): string {
  if (path.startsWith('http://') || path.startsWith('https://')) return path;
  return apiBase + path;
}

export async function apiFetch(path: string, init?: RequestInit): Promise<Response> {
  return fetch(apiUrl(path), init);
}

export async function apiJson<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await apiFetch(path, init);
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
  return res.json() as Promise<T>;
}

export function wsUrl(path: string): string {
  const u = new URL(apiUrl(path), location.href);
  u.protocol = u.protocol === 'https:' ? 'wss:' : 'ws:';
  return u.toString();
}
