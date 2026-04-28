import { writable, derived } from 'svelte/store';
import { apiJson, apiFetch, apiUrl } from '$lib/api';

export interface PresetVersion {
  tag: string;
  label?: string;
}

export interface Preset {
  name: string;
  description?: string;
  image?: string;
  dashboard?: string;
  depends_on?: string[];
  missing_deps?: string[];
  installed?: boolean;
  installed_tags?: string[];
  versions?: PresetVersion[];
  default_version?: string;
  selected_version?: string;
  installing?: boolean;
  installingPhase?: string;
  installingMessage?: string;
  installingDep?: string;
  error?: string;
}

export const presets = writable<Preset[]>([]);
export const presetsLoaded = writable<boolean>(false);

export async function loadPresets() {
  try {
    const data = await apiJson<Preset[]>('/api/services/presets');
    const prev = pget(presets);
    const next = (data || []).map((p) => {
      const old = prev.find((x) => x.name === p.name) || ({} as Preset);
      const enriched: Preset = { ...p, installing: old.installing || false, error: old.error || '' };
      if ((enriched.versions || []).length > 0) {
        const installed = new Set(enriched.installed_tags || []);
        const avail = (enriched.versions || []).filter((v) => !installed.has(v.tag));
        const stillValid = avail.find((v) => v.tag === old.selected_version);
        const fallback = avail.find((v) => v.tag === enriched.default_version) || avail[0];
        enriched.selected_version = (stillValid || fallback || { tag: '' }).tag;
      }
      return enriched;
    });
    presets.set(next);
    presetsLoaded.set(true);
  } catch {
    /* keep previous */
  }
}

function pget<T>(s: import('svelte/store').Readable<T>): T {
  let v!: T;
  const u = s.subscribe((x) => (v = x));
  u();
  return v;
}

export const installablePresets = derived(presets, ($p) =>
  $p.filter((p) => {
    if ((p.missing_deps || []).length > 0) return false;
    if ((p.versions || []).length > 0) {
      return (p.versions || []).length > (p.installed_tags || []).length;
    }
    return !p.installed;
  })
);

export function availableVersions(p: Preset): PresetVersion[] {
  const installed = new Set(p.installed_tags || []);
  return (p.versions || []).filter((v) => !installed.has(v.tag));
}

export function phaseLabel(p: Preset): string {
  if (!p.installing) return 'Add';
  switch (p.installingPhase) {
    case 'installing_config':
      return 'Writing config...';
    case 'starting_deps':
      return p.installingDep ? 'Starting ' + p.installingDep + '...' : 'Starting dependencies...';
    case 'pulling_image':
      return 'Pulling image...';
    case 'starting_unit':
      return 'Starting service...';
    case 'waiting_ready':
      return 'Waiting for ready...';
    default:
      return 'Adding...';
  }
}

function updatePreset(name: string, mut: (p: Preset) => Preset) {
  presets.update((list) => list.map((p) => (p.name === name ? mut(p) : p)));
}

export interface InstallResult {
  ok: boolean;
  name?: string;
  error?: string;
}

export async function installPreset(p: Preset): Promise<InstallResult> {
  if ((p.missing_deps || []).length > 0) {
    const err = 'Install ' + (p.missing_deps || []).join(', ') + ' first';
    updatePreset(p.name, (x) => ({ ...x, error: err }));
    return { ok: false, error: err };
  }

  updatePreset(p.name, (x) => ({
    ...x,
    installing: true,
    installingPhase: '',
    installingMessage: '',
    installingDep: '',
    error: ''
  }));

  let url = '/api/services/presets/' + encodeURIComponent(p.name);
  if ((p.versions || []).length > 0 && p.selected_version) {
    url += '?version=' + encodeURIComponent(p.selected_version);
  }

  let finalEvent: { phase?: string; name?: string; error?: string } | null = null;

  try {
    const res = await apiFetch(url, { method: 'POST' });
    if (!res.ok || !res.body) {
      const text = (await res.text()).trim() || 'install failed';
      updatePreset(p.name, (x) => ({ ...x, installing: false, error: text }));
      return { ok: false, error: text };
    }
    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';
    while (true) {
      const { value, done } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      let nl: number;
      while ((nl = buffer.indexOf('\n')) !== -1) {
        const line = buffer.slice(0, nl).trim();
        buffer = buffer.slice(nl + 1);
        if (!line) continue;
        let evt: { phase?: string; dep?: string; image?: string; message?: string; name?: string; error?: string };
        try {
          evt = JSON.parse(line);
        } catch {
          continue;
        }
        if (evt.phase === 'done' || evt.phase === 'error') {
          finalEvent = evt;
          continue;
        }
        updatePreset(p.name, (x) => ({
          ...x,
          installingPhase: evt.phase || '',
          installingDep: evt.phase === 'starting_deps' ? evt.dep || '' : x.installingDep,
          installingMessage:
            evt.phase === 'pulling_image' ? evt.message || (evt.image ? 'pulling ' + evt.image : '') : ''
        }));
      }
    }
    if (!finalEvent || finalEvent.phase === 'error') {
      const err = finalEvent?.error || 'install failed without a final result';
      updatePreset(p.name, (x) => ({ ...x, installing: false, error: err }));
      return { ok: false, error: err };
    }
    updatePreset(p.name, (x) => ({
      ...x,
      installing: false,
      installingPhase: '',
      installingMessage: '',
      installingDep: ''
    }));
    return { ok: true, name: finalEvent.name || p.name };
  } catch (e) {
    const err = e instanceof Error ? e.message : 'Request failed';
    updatePreset(p.name, (x) => ({ ...x, installing: false, error: err }));
    return { ok: false, error: err };
  }
}

// exported for tests
export const _apiUrl = apiUrl;
