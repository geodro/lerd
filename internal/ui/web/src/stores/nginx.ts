import { apiJson, apiFetch } from '$lib/api';

export interface NginxConfig {
  path: string;
  content: string;
}

export async function getNginxConfig(): Promise<NginxConfig> {
  return apiJson<NginxConfig>('/api/nginx/config');
}

export async function saveNginxConfig(content: string): Promise<boolean> {
  try {
    const res = await apiFetch('/api/nginx/config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content })
    });
    return res.ok;
  } catch {
    return false;
  }
}
