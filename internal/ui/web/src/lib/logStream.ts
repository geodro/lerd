import { writable } from 'svelte/store';
import { apiUrl } from './api';

export interface LogStream {
  lines: ReturnType<typeof writable<string[]>>;
  connected: ReturnType<typeof writable<boolean>>;
  connect: () => void;
  close: () => void;
  clear: () => void;
}

export function createLogStream(path: string, maxLines = 500): LogStream {
  const lines = writable<string[]>([]);
  const connected = writable<boolean>(false);
  let source: EventSource | null = null;

  function close() {
    if (source) {
      source.close();
      source = null;
    }
    connected.set(false);
  }

  function clear() {
    lines.set([]);
  }

  function connect() {
    close();
    clear();
    try {
      const es = new EventSource(apiUrl(path));
      source = es;
      es.addEventListener('open', () => connected.set(true));
      es.addEventListener('error', () => connected.set(false));
      es.addEventListener('message', (e) => {
        lines.update((l) => {
          const next = l.length >= maxLines ? l.slice(l.length - maxLines + 1) : l.slice();
          next.push((e as MessageEvent).data);
          return next;
        });
      });
    } catch {
      connected.set(false);
    }
  }

  return { lines, connected, connect, close, clear };
}
