import { writable, type Writable } from 'svelte/store';
import { apiUrl } from './api';

// DumpEvent mirrors internal/dumps.Event verbatim. Keep field names in sync
// with the Go struct's json tags — TypeScript validates wire shape, Go owns
// the source of truth.
export interface DumpSource {
  file: string;
  line: number;
}

export interface DumpContext {
  type: 'fpm' | 'cli' | string;
  site?: string;
  branch?: string;
  domain?: string;
  request?: string;
  pid?: number;
}

export interface DumpEvent {
  v: number;
  id: string;
  ts: string;
  kind: string;
  ctx: DumpContext;
  src: DumpSource;
  label?: string;
  text?: string;
  // tree is opaque JSON the receiver passes through unchanged. Deferred to
  // a later PR; the current bridge only ships `text`.
  tree?: unknown;
  trunc?: boolean;
}

export interface DumpsStream {
  events: Writable<DumpEvent[]>;
  connected: Writable<boolean>;
  connect: () => void;
  close: () => void;
  clear: () => void;
}

const DEFAULT_MAX = 500;

export function createDumpsStream(query: Record<string, string> = {}, maxEvents = DEFAULT_MAX): DumpsStream {
  const events = writable<DumpEvent[]>([]);
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
    events.set([]);
  }

  function buildPath() {
    const params = new URLSearchParams();
    for (const [k, v] of Object.entries(query)) {
      if (v) params.set(k, v);
    }
    const qs = params.toString();
    return qs ? `/api/dumps/stream?${qs}` : '/api/dumps/stream';
  }

  function append(ev: DumpEvent) {
    events.update((list) => {
      // De-dupe by ID. The replay-on-reconnect path can resend events the
      // browser already has if Last-Event-ID isn't honoured by an
      // intermediate proxy.
      if (list.some((e) => e.id === ev.id)) return list;
      const next = list.length >= maxEvents ? list.slice(list.length - maxEvents + 1) : list.slice();
      next.push(ev);
      return next;
    });
  }

  function connect() {
    close();
    try {
      const es = new EventSource(apiUrl(buildPath()));
      source = es;
      es.addEventListener('open', () => connected.set(true));
      es.addEventListener('error', () => connected.set(false));
      es.addEventListener('message', (e) => {
        try {
          const data = JSON.parse((e as MessageEvent).data) as DumpEvent;
          if (data && typeof data === 'object' && data.id) append(data);
        } catch {
          // Malformed payload — ignore. The Go server only emits valid JSON,
          // but proxies could rewrite the stream and we'd rather skip a line
          // than crash the tab.
        }
      });
    } catch {
      connected.set(false);
    }
  }

  return { events, connected, connect, close, clear };
}
