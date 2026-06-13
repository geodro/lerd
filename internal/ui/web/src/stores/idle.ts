import { writable } from 'svelte/store';
import { apiJson, apiFetch } from '$lib/api';

// Global idle-suspend policy (a single on/off + timeout, not per site).
export const idleEnabled = writable<boolean>(false);
export const idleTimeoutMinutes = writable<number>(30);

interface SettingsResponse {
  idle_suspend_enabled?: boolean;
  idle_suspend_timeout_minutes?: number;
}

export async function loadIdle() {
  try {
    const res = await apiJson<SettingsResponse>('/api/settings');
    idleEnabled.set(Boolean(res.idle_suspend_enabled));
    if (typeof res.idle_suspend_timeout_minutes === 'number' && res.idle_suspend_timeout_minutes > 0) {
      idleTimeoutMinutes.set(res.idle_suspend_timeout_minutes);
    }
  } catch {
    /* keep previous */
  }
}

export async function saveIdle(enabled: boolean, timeoutMinutes: number): Promise<boolean> {
  try {
    const res = await apiFetch('/api/settings/idle-suspend', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enabled, timeout_minutes: timeoutMinutes })
    });
    if (res.ok) {
      idleEnabled.set(enabled);
      idleTimeoutMinutes.set(timeoutMinutes);
    }
    return res.ok;
  } catch {
    return false;
  }
}
