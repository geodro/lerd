import { writable } from 'svelte/store';
import { apiJson } from '$lib/api';

export interface AccessMode {
  loopback: boolean;
  lanExposed: boolean;
  checked: boolean;
}

export const accessMode = writable<AccessMode>({
  loopback: true,
  lanExposed: false,
  checked: false
});

interface AccessModeResponse {
  loopback?: boolean;
  lan_exposed?: boolean;
}

export async function loadAccessMode() {
  try {
    const res = await apiJson<AccessModeResponse>('/api/access-mode');
    accessMode.set({
      loopback: Boolean(res.loopback),
      lanExposed: Boolean(res.lan_exposed),
      checked: true
    });
  } catch {
    accessMode.update((a) => ({ ...a, checked: true }));
  }
}
