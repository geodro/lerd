import { apiJson } from '$lib/api';

export interface BrowseResult {
  current: string;
  dirs: Array<{ name: string; path: string }>;
  error?: string;
}

export async function browseDir(dir: string): Promise<BrowseResult> {
  try {
    const res = await apiJson<BrowseResult>('/api/browse?dir=' + encodeURIComponent(dir));
    return { current: res.current ?? '', dirs: res.dirs ?? [], error: res.error };
  } catch (e) {
    return { current: '', dirs: [], error: e instanceof Error ? e.message : 'Browse failed' };
  }
}
