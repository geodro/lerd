import { apiFetch } from '$lib/api';
import type { Site } from './sites';

interface ActionResult {
  ok: boolean;
  error?: string;
}

async function post(path: string): Promise<ActionResult> {
  try {
    const res = await apiFetch(path, { method: 'POST' });
    const data = (await res.json()) as ActionResult;
    return { ok: Boolean(data.ok), error: data.error };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'Request failed' };
  }
}

export function addDomain(site: Site, name: string) {
  return post(
    `/api/sites/${encodeURIComponent(site.domain)}/domain:add?name=${encodeURIComponent(name)}`
  );
}

export function editDomain(site: Site, oldName: string, newName: string) {
  return post(
    `/api/sites/${encodeURIComponent(site.domain)}/domain:edit?old=${encodeURIComponent(
      oldName
    )}&new=${encodeURIComponent(newName)}`
  );
}

export function removeDomain(site: Site, name: string) {
  return post(
    `/api/sites/${encodeURIComponent(site.domain)}/domain:remove?name=${encodeURIComponent(name)}`
  );
}
