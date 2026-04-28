import { apiJson } from '$lib/api';

export interface AppLogFile {
  name: string;
  size?: number;
  modified?: string;
}

export interface AppLogEntry {
  level?: string;
  date?: string;
  message?: string;
  detail?: string;
}

export async function listAppLogFiles(domain: string): Promise<AppLogFile[]> {
  try {
    const res = await apiJson<{ files?: AppLogFile[] }>(`/api/app-logs/${encodeURIComponent(domain)}`);
    return Array.isArray(res.files) ? res.files : [];
  } catch {
    return [];
  }
}

export async function loadAppLogEntries(
  domain: string,
  file: string,
  showAll: boolean
): Promise<AppLogEntry[]> {
  try {
    const limit = showAll ? 0 : 100;
    const res = await apiJson<{ entries?: AppLogEntry[] }>(
      `/api/app-logs/${encodeURIComponent(domain)}/${encodeURIComponent(file)}?limit=${limit}`
    );
    return Array.isArray(res.entries) ? res.entries : [];
  } catch {
    return [];
  }
}
