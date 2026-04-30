import { writable } from 'svelte/store';
import { apiJson, apiFetch } from '$lib/api';
import { wsMessage } from '$lib/ws';

export interface UnhealthyWorker {
  site: string;
  worker: string;
  unit: string;
  state: string;
}

interface HealthResponse {
  unhealthy?: UnhealthyWorker[];
  error?: string;
}

interface HealEvent {
  phase: 'starting' | 'healed' | 'failed' | 'done';
  site?: string;
  unit?: string;
  error?: string;
}

export const unhealthyWorkers = writable<UnhealthyWorker[]>([]);
export const healLoading = writable(false);
export const healProgressUnit = writable<string | null>(null);
export const healDoneCount = writable(0);
export const healTotalCount = writable(0);

// loadWorkerHealth GETs the current snapshot. Backed by the existing
// 3-second batched unit-state cache on the server, so polling on every
// site-list refresh is cheap.
export async function loadWorkerHealth() {
  try {
    const res = await apiJson<HealthResponse>('/api/workers/health');
    unhealthyWorkers.set(res.unhealthy ?? []);
  } catch {
    /* keep previous */
  }
}

// The server pushes `unhealthy_workers` as a top-level WS field whenever
// the health-watcher detects the set has changed (it diffs a cached
// detector every 5s; idle tabs skip entirely). No extra HTTP round-trip,
// no extra subprocess: the watcher reads the same 3-second-TTL unit-state
// cache the sites snapshot already uses. The fall-back loadWorkerHealth()
// stays available for non-WS callers (initial mount, post-heal refresh).
wsMessage.subscribe((msg) => {
  if (!msg) return;
  if (Array.isArray(msg.unhealthy_workers)) {
    unhealthyWorkers.set(msg.unhealthy_workers as UnhealthyWorker[]);
  }
});

// healAll triggers the server-side detector + reset-failed + start sequence
// for every unhealthy worker. Reads the NDJSON stream so the banner can
// show per-unit progress instead of a blank spinner. Returns once the
// "done" event arrives; the caller usually re-runs loadWorkerHealth to
// confirm the count went to zero.
export async function healAll(): Promise<{ ok: boolean; healed: number; failed: number; error?: string }> {
  // Snapshot the count up front so "Healing X of Y" stays stable even if
  // the underlying unhealthyWorkers list gets refreshed mid-stream.
  let total = 0;
  unhealthyWorkers.subscribe((w) => (total = w.length))();
  healLoading.set(true);
  healProgressUnit.set(null);
  healDoneCount.set(0);
  healTotalCount.set(total);
  let healed = 0;
  let failed = 0;
  try {
    const res = await apiFetch('/api/workers/heal', { method: 'POST' });
    if (!res.ok || !res.body) {
      return { ok: false, healed, failed, error: `${res.status} ${res.statusText}` };
    }
    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buf = '';
    let lastError: string | undefined;
    let sawDone = false;
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      let nl: number;
      while ((nl = buf.indexOf('\n')) >= 0) {
        const line = buf.slice(0, nl).trim();
        buf = buf.slice(nl + 1);
        if (!line) continue;
        let evt: HealEvent;
        try {
          evt = JSON.parse(line) as HealEvent;
        } catch {
          continue;
        }
        switch (evt.phase) {
          case 'starting':
            healProgressUnit.set(evt.unit ?? null);
            break;
          case 'healed':
            healed++;
            healDoneCount.set(healed + failed);
            break;
          case 'failed':
            // Without a unit the failure is the detector itself.
            if (evt.unit) {
              failed++;
              healDoneCount.set(healed + failed);
            } else {
              lastError = evt.error;
            }
            break;
          case 'done':
            sawDone = true;
            break;
        }
      }
    }
    return { ok: sawDone && !lastError, healed, failed, error: lastError };
  } catch (e) {
    return { ok: false, healed, failed, error: e instanceof Error ? e.message : 'request failed' };
  } finally {
    healLoading.set(false);
    healProgressUnit.set(null);
  }
}
