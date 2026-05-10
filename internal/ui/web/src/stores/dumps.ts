import { derived, writable, get, type Readable } from 'svelte/store';
import { apiFetch, apiJson } from '$lib/api';
import { createDumpsStream, type DumpEvent } from '$lib/dumpsStream';

export interface DumpsStatus {
  enabled: boolean;
  listening: boolean;
  addr: string;
  count: number;
  subscribers: number;
  last_ts: string;
}

const stream = createDumpsStream();

export const dumps = stream.events;
export const dumpsConnected = stream.connected;

export const status = writable<DumpsStatus | null>(null);
export const filterSite = writable<string>('');
export const filterCtx = writable<'' | 'fpm' | 'cli'>('');
export const filterText = writable<string>('');

// Group dumps by request when ctx.type === 'fpm', or by pid+ts-bucket for cli.
// Web tab can render one card per group.
export interface DumpGroup {
  key: string;
  label: string;
  events: DumpEvent[];
  ts: string;
}

export const dumpGroups: Readable<DumpGroup[]> = derived(
  [dumps, filterSite, filterCtx, filterText],
  ([$dumps, $site, $ctx, $text]) => {
    const filtered = $dumps.filter((ev) => {
      if ($site && ev.ctx.site !== $site) return false;
      if ($ctx && ev.ctx.type !== $ctx) return false;
      if ($text) {
        const haystack = [ev.label ?? '', ev.text ?? '', ev.src.file ?? ''].join(' ').toLowerCase();
        if (!haystack.includes($text.toLowerCase())) return false;
      }
      return true;
    });
    const groups = new Map<string, DumpGroup>();
    for (const ev of filtered) {
      const key = groupKey(ev);
      const existing = groups.get(key);
      if (existing) {
        existing.events.push(ev);
      } else {
        groups.set(key, {
          key,
          label: groupLabel(ev),
          events: [ev],
          ts: ev.ts
        });
      }
    }
    // Most recent group first.
    return Array.from(groups.values()).sort((a, b) => b.ts.localeCompare(a.ts));
  }
);

function groupKey(ev: DumpEvent): string {
  if (ev.ctx.type === 'fpm') {
    return `fpm:${ev.ctx.site ?? ''}:${ev.ctx.request ?? ''}:${ev.ctx.pid ?? ''}`;
  }
  // CLI events tend to come from a single artisan invocation; cluster them
  // into 5-second buckets so a tinker session shows as one card.
  const bucket = Math.floor(new Date(ev.ts).getTime() / 5000);
  return `cli:${ev.ctx.site ?? ''}:${ev.ctx.pid ?? ''}:${bucket}`;
}

function groupLabel(ev: DumpEvent): string {
  if (ev.ctx.type === 'fpm') {
    const req = ev.ctx.request || '(request)';
    const site = ev.ctx.site ? `[${ev.ctx.site}] ` : '';
    return site + req;
  }
  const site = ev.ctx.site ? `[${ev.ctx.site}] ` : '';
  return `${site}cli (pid ${ev.ctx.pid ?? '?'})`;
}

let started = false;

export function startDumpsStream() {
  if (started) return;
  started = true;
  stream.connect();
  void refreshStatus();
}

export function stopDumpsStream() {
  if (!started) return;
  started = false;
  stream.close();
}

export async function refreshStatus(): Promise<void> {
  try {
    const data = await apiJson<DumpsStatus>('/api/dumps/status');
    status.set(data);
  } catch {
    status.set(null);
  }
}

export async function clearDumps(): Promise<void> {
  await apiFetch('/api/dumps/clear', { method: 'POST' });
  stream.clear();
  void refreshStatus();
}

export async function toggleDumps(enable: boolean): Promise<void> {
  await apiFetch('/api/dumps/toggle', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enable })
  });
  void refreshStatus();
}

// Derived list of unique site names seen in the buffered events, for the
// filter dropdown. Sites without explicit names (e.g. when DOCUMENT_ROOT is
// unusual) appear as "(unknown)".
export const knownSites: Readable<string[]> = derived(dumps, ($dumps) => {
  const set = new Set<string>();
  for (const ev of $dumps) {
    set.add(ev.ctx.site || '');
  }
  return Array.from(set).sort();
});

// snapshot returns the current event list. Used by tests that don't want to
// subscribe to the store.
export function snapshot(): DumpEvent[] {
  return get(dumps);
}
