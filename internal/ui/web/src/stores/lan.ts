import { writable } from 'svelte/store';
import { apiJson, apiFetch } from '$lib/api';

export interface LANStatus {
  exposed: boolean;
  lanIP: string;
  macos: boolean;
  loaded: boolean;
  progressSteps: string[];
  loading: boolean;
  error: string;
  justExposed: boolean;
  setupCode: string;
  setupCurl: string;
  setupExpiresIn: string;
  setupLoading: boolean;
  setupError: string;
  setupCopied: boolean;
}

const empty: LANStatus = {
  exposed: false,
  lanIP: '',
  macos: false,
  loaded: false,
  progressSteps: [],
  loading: false,
  error: '',
  justExposed: false,
  setupCode: '',
  setupCurl: '',
  setupExpiresIn: '',
  setupLoading: false,
  setupError: '',
  setupCopied: false
};

export const lan = writable<LANStatus>(empty);

function patch(up: Partial<LANStatus>) {
  lan.update((v) => ({ ...v, ...up }));
}

interface StatusResponse {
  exposed?: boolean;
  lan_ip?: string;
  macos?: boolean;
}

export async function loadLANStatus() {
  try {
    const data = await apiJson<StatusResponse>('/api/lan/status');
    patch({ exposed: Boolean(data.exposed), lanIP: data.lan_ip || '', macos: Boolean(data.macos), loaded: true });
  } catch {
    patch({ loaded: true });
  }
}

export async function toggleLAN(action: 'expose' | 'unexpose') {
  patch({ loading: true, error: '', progressSteps: [] });
  try {
    const res = await apiFetch('/api/lan/status', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action })
    });
    if (!res.ok || !res.body) {
      const text = (await res.text()).trim();
      patch({ loading: false, error: text || 'toggle failed' });
      return;
    }
    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buf = '';
    let finalEvent: { result?: string; exposed?: boolean; lan_ip?: string; error?: string } | null = null;
    while (true) {
      const { value, done } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      let nl: number;
      while ((nl = buf.indexOf('\n')) !== -1) {
        const line = buf.slice(0, nl).trim();
        buf = buf.slice(nl + 1);
        if (!line) continue;
        try {
          const evt = JSON.parse(line) as {
            step?: string;
            result?: string;
            exposed?: boolean;
            lan_ip?: string;
            error?: string;
          };
          if (evt.step) {
            lan.update((v) => ({ ...v, progressSteps: [...v.progressSteps, evt.step!] }));
          } else if (evt.result) {
            finalEvent = evt;
          }
        } catch {
          /* malformed, skip */
        }
      }
    }
    if (!finalEvent || finalEvent.result === 'error') {
      patch({
        loading: false,
        error: finalEvent?.error || 'Toggle failed without a final result'
      });
      return;
    }
    patch({
      loading: false,
      exposed: Boolean(finalEvent.exposed),
      lanIP: finalEvent.lan_ip || '',
      justExposed: action === 'expose',
      setupCode: action === 'unexpose' ? '' : (undefined as unknown as string),
      setupCurl: action === 'unexpose' ? '' : (undefined as unknown as string),
      setupError: action === 'unexpose' ? '' : (undefined as unknown as string)
    });
  } catch (e) {
    patch({ loading: false, error: e instanceof Error ? e.message : 'Failed to toggle LAN exposure' });
  }
}

export async function generateRemoteSetupCode() {
  patch({ setupLoading: true, setupError: '', setupCopied: false });
  try {
    const res = await apiFetch('/api/remote-setup/generate', { method: 'POST' });
    if (!res.ok) {
      const text = (await res.text()).trim();
      patch({ setupLoading: false, setupError: text || 'Generate failed' });
      return;
    }
    const data = (await res.json()) as { code?: string; curl?: string; expires_in?: string };
    patch({
      setupLoading: false,
      setupCode: data.code || '',
      setupCurl: data.curl || '',
      setupExpiresIn: data.expires_in || ''
    });
  } catch (e) {
    patch({ setupLoading: false, setupError: e instanceof Error ? e.message : 'Request failed' });
  }
}

export async function copySetupCurl() {
  const current = getSetupCurl();
  if (!current) return;
  try {
    await navigator.clipboard.writeText(current);
    patch({ setupCopied: true });
    setTimeout(() => patch({ setupCopied: false }), 1500);
  } catch {
    /* no-op */
  }
}

function getSetupCurl(): string {
  let v = '';
  const u = lan.subscribe((x) => (v = x.setupCurl));
  u();
  return v;
}
