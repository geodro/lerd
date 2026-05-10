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
        // Track the latest event timestamp on the group so sorting reflects
        // the most-recent activity in that request, not its first dump.
        if (ev.ts > existing.ts) existing.ts = ev.ts;
      } else {
        groups.set(key, {
          key,
          label: groupLabel(ev),
          events: [ev],
          ts: ev.ts
        });
      }
    }
    // Newest first, end to end: groups by latest activity, and events
    // within each group in reverse arrival order so the most recent dump
    // sits at the top of every card.
    const out = Array.from(groups.values()).sort((a, b) => b.ts.localeCompare(a.ts));
    for (const g of out) {
      g.events = g.events.slice().reverse();
    }
    return out;
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

// lastFlashId tracks the most recent event arriving over the live socket
// (post-initial-replay) so DumpEntry can paint a one-shot highlight ring
// that fades over a couple of seconds. Cleared via setTimeout so the ring
// animation only plays once per genuinely new dump.
export const lastFlashId = writable<string>('');

// Window during which incoming events count as part of the snapshot replay,
// not new live deliveries. Picked so a slow round-trip on a busy machine
// still drops every replayed event into the "stale" bucket.
const REPLAY_GRACE_MS = 400;
const FLASH_DURATION_MS = 2500;

let flashReady = false;
let flashTimer: ReturnType<typeof setTimeout> | null = null;
let lastSeenId = '';

dumps.subscribe(($dumps) => {
  if (!flashReady) {
    return;
  }
  if ($dumps.length === 0) {
    return;
  }
  const latest = $dumps[$dumps.length - 1];
  if (latest.id === lastSeenId) {
    return;
  }
  lastSeenId = latest.id;
  lastFlashId.set(latest.id);
  if (flashTimer) clearTimeout(flashTimer);
  flashTimer = setTimeout(() => lastFlashId.set(''), FLASH_DURATION_MS);
});

let started = false;

export function startDumpsStream() {
  if (started) return;
  started = true;
  // Seed lastSeenId with whatever's already in the store so the initial
  // replay's last event doesn't immediately flash on connect.
  const snap = get(dumps);
  if (snap.length > 0) {
    lastSeenId = snap[snap.length - 1].id;
  }
  setTimeout(() => {
    // After the replay grace window, future store updates count as live.
    flashReady = true;
    const after = get(dumps);
    if (after.length > 0) {
      lastSeenId = after[after.length - 1].id;
    }
  }, REPLAY_GRACE_MS);
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
