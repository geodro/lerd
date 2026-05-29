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

/**
 * Decode a response that handlers sometimes return as JSON envelopes and
 * sometimes as plain-text error bodies (via http.Error). Returns the parsed
 * JSON when possible; otherwise wraps the text body as { ok:false, error }.
 */
export async function decodeJSONResult<T extends { ok?: boolean; error?: string }>(
  res: Response
): Promise<T> {
  const text = await res.text();
  if (text) {
    try {
      return JSON.parse(text) as T;
    } catch {
      /* fall through to text-body envelope */
    }
  }
  const fallback: { ok: boolean; error: string } = {
    ok: false,
    error: text.trim() || `${res.status} ${res.statusText}`
  };
  return fallback as T;
}

export function wsUrl(path: string): string {
  const u = new URL(apiUrl(path), location.href);
  u.protocol = u.protocol === 'https:' ? 'wss:' : 'ws:';
  return u.toString();
}
