import { writable } from 'svelte/store';
import { apiJson, apiFetch } from '$lib/api';

export interface RemoteControl {
  enabled: boolean;
  username: string;
  loading: boolean;
  error: string;
}

const empty: RemoteControl = { enabled: false, username: '', loading: false, error: '' };

export const remoteControl = writable<RemoteControl>(empty);

interface RemoteControlResponse {
  enabled?: boolean;
  username?: string;
  error?: string;
}

export async function loadRemoteControl() {
  try {
    const data = await apiJson<RemoteControlResponse>('/api/remote-control');
    remoteControl.set({
      enabled: Boolean(data.enabled),
      username: data.username || '',
      loading: false,
      error: ''
    });
  } catch (e) {
    remoteControl.update((v) => ({
      ...v,
      error: e instanceof Error ? e.message : 'Failed to load remote-control state'
    }));
  }
}

export async function enableRemoteControl(username: string, password: string): Promise<{ ok: boolean; error?: string }> {
  remoteControl.update((v) => ({ ...v, loading: true, error: '' }));
  try {
    const res = await apiFetch('/api/remote-control', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enabled: true, username, password })
    });
    const data = (await res.json()) as { ok?: boolean; error?: string };
    if (data.ok) {
      remoteControl.set({ enabled: true, username, loading: false, error: '' });
      return { ok: true };
    }
    remoteControl.update((v) => ({ ...v, loading: false, error: data.error || 'Failed' }));
    return { ok: false, error: data.error };
  } catch (e) {
    const err = e instanceof Error ? e.message : 'Request failed';
    remoteControl.update((v) => ({ ...v, loading: false, error: err }));
    return { ok: false, error: err };
  }
}

export async function disableRemoteControl(): Promise<boolean> {
  remoteControl.update((v) => ({ ...v, loading: true, error: '' }));
  try {
    const res = await apiFetch('/api/remote-control', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enabled: false })
    });
    const data = (await res.json()) as { ok?: boolean; error?: string };
    if (data.ok) {
      remoteControl.set({ enabled: false, username: '', loading: false, error: '' });
      return true;
    }
    remoteControl.update((v) => ({ ...v, loading: false, error: data.error || 'Failed' }));
    return false;
  } catch (e) {
    remoteControl.update((v) => ({ ...v, loading: false, error: e instanceof Error ? e.message : 'Request failed' }));
    return false;
  }
}
