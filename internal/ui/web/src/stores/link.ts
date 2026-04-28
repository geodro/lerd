import { apiFetch } from '$lib/api';

export interface LinkEvent {
  line?: string;
  done?: boolean;
  ok?: boolean;
  domain?: string;
  error?: string;
}

export async function streamLinkSite(path: string, onEvent: (e: LinkEvent) => void): Promise<void> {
  const res = await apiFetch('/api/sites/link?path=' + encodeURIComponent(path), { method: 'POST' });
  if (!res.body) throw new Error('no response body');
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buf = '';
  let eventType = '';

  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    const lines = buf.split('\n');
    buf = lines.pop() ?? '';
    for (const line of lines) {
      if (line.startsWith('event: ')) {
        eventType = line.slice(7);
      } else if (line.startsWith('data: ')) {
        const payload = line.slice(6);
        if (eventType === 'done') {
          try {
            const result = JSON.parse(payload) as { ok?: boolean; domain?: string; error?: string };
            onEvent({ done: true, ok: Boolean(result.ok), domain: result.domain, error: result.error });
          } catch {
            onEvent({ done: true, ok: false, error: 'bad done payload' });
          }
        } else {
          onEvent({ line: payload });
        }
        eventType = '';
      } else if (line === '') {
        eventType = '';
      }
    }
  }
}
