import { writable } from 'svelte/store';
import { apiJson, apiFetch } from '$lib/api';

export const nodeVersions = writable<string[]>([]);

export async function loadNodeVersions() {
  try {
    const list = await apiJson<string[]>('/api/node-versions');
    nodeVersions.set(Array.isArray(list) ? list : []);
  } catch {
    /* keep previous */
  }
}

export async function setDefaultNode(v: string): Promise<boolean> {
  try {
    const res = await apiFetch('/api/node-versions/' + encodeURIComponent(v) + '/set-default', {
      method: 'POST'
    });
    if (res.ok) await loadNodeVersions();
    return res.ok;
  } catch {
    return false;
  }
}

export async function removeNode(v: string): Promise<boolean> {
  try {
    const res = await apiFetch('/api/node-versions/' + encodeURIComponent(v) + '/remove', {
      method: 'POST'
    });
    if (res.ok) await loadNodeVersions();
    return res.ok;
  } catch {
    return false;
  }
}

export async function installNode(v: string): Promise<boolean> {
  try {
    const res = await apiFetch('/api/node-versions/install', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version: v })
    });
    if (res.ok) await loadNodeVersions();
    return res.ok;
  } catch {
    return false;
  }
}
