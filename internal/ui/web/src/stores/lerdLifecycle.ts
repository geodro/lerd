import { writable } from 'svelte/store';
import { apiFetch } from '$lib/api';
import { loadStatus } from './status';

export const lerdStarting = writable<boolean>(false);
export const lerdStopping = writable<boolean>(false);

export async function lerdStart(): Promise<boolean> {
  lerdStarting.set(true);
  try {
    const res = await apiFetch('/api/lerd/start', { method: 'POST' });
    await loadStatus();
    return res.ok;
  } catch {
    return false;
  } finally {
    lerdStarting.set(false);
  }
}

export async function lerdStop(): Promise<boolean> {
  lerdStopping.set(true);
  try {
    const res = await apiFetch('/api/lerd/stop', { method: 'POST' });
    await loadStatus();
    return res.ok;
  } catch {
    return false;
  } finally {
    lerdStopping.set(false);
  }
}
