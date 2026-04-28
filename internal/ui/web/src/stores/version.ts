import { writable } from 'svelte/store';
import { apiJson } from '$lib/api';

export interface VersionInfo {
  current: string;
  latest: string;
  hasUpdate: boolean;
  checked: boolean;
  checking: boolean;
  changelog: string;
}

const empty: VersionInfo = {
  current: '...',
  latest: '',
  hasUpdate: false,
  checked: false,
  checking: false,
  changelog: ''
};

export const version = writable<VersionInfo>(empty);

interface VersionResponse {
  current?: string;
  latest?: string;
  has_update?: boolean;
  changelog?: string;
}

export async function loadVersion() {
  try {
    const res = await apiJson<VersionResponse>('/api/version');
    version.set({
      current: res.current ?? '...',
      latest: res.latest ?? '',
      hasUpdate: Boolean(res.has_update),
      checked: true,
      checking: false,
      changelog: res.changelog ?? ''
    });
  } catch {
    version.update((v) => ({ ...v, checking: false }));
  }
}
