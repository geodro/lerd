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

/** Group an existing secondary site under main at the given subdomain label. */
export function assignGroup(main: Site, secondaryDomain: string, label: string, shareDB = false) {
  return post(
    `/api/sites/${encodeURIComponent(main.domain)}/group:assign?secondary=${encodeURIComponent(
      secondaryDomain
    )}&label=${encodeURIComponent(label)}${shareDB ? '&share_db=1' : ''}`
  );
}

/** Toggle whether a secondary shares the group main's database. */
export function setGroupSharedDB(secondary: Site, share: boolean) {
  return post(
    `/api/sites/${encodeURIComponent(secondary.domain)}/group:set-db?share=${share ? '1' : '0'}`
  );
}

/** Ungroup a secondary site, restoring its standalone domain. */
export function unassignGroup(secondary: Site) {
  return post(`/api/sites/${encodeURIComponent(secondary.domain)}/group:unassign`);
}

/** Change the subdomain label of a secondary site. */
export function setGroupLabel(secondary: Site, label: string) {
  return post(
    `/api/sites/${encodeURIComponent(secondary.domain)}/group:set-label?label=${encodeURIComponent(
      label
    )}`
  );
}

/** Dissolve the whole group the given site belongs to. */
export function dissolveGroup(site: Site) {
  return post(`/api/sites/${encodeURIComponent(site.domain)}/group:remove`);
}
