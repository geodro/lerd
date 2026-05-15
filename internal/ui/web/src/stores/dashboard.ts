import { writable, derived, get } from 'svelte/store';
import { services, type Service } from './services';

export interface DashboardRef {
  name: string;
  label?: string;
  dashboard: string;
  // extraPath is appended to dashboard for the iframe src, used to deep-link
  // a service overlay (e.g. mailpit's /view/{id} for a captured email).
  extraPath?: string;
}

// The currently-open dashboard, either a real service or the synthetic 'docs' ref.
export const dashboardOpen = writable<DashboardRef | null>(null);

const DOCS_REF: DashboardRef = {
  name: 'docs',
  label: 'Documentation',
  dashboard: 'https://geodro.github.io/lerd/'
};

function fallbackHash(): string {
  const h = location.hash.slice(1);
  for (const t of ['sites', 'services', 'system']) {
    if (h === t || h.startsWith(t + '/')) return t;
  }
  return 'sites';
}

export function openDashboard(svc: Service) {
  if (svc.dashboard_external && svc.dashboard) {
    window.open(svc.dashboard, '_blank', 'noopener,noreferrer');
    return;
  }
  if (!svc.dashboard) return;
  const cur = get(dashboardOpen);
  if (cur && cur.name === svc.name) {
    dashboardOpen.set(null);
    location.hash = fallbackHash();
    return;
  }
  dashboardOpen.set({ name: svc.name, label: svc.name, dashboard: svc.dashboard });
  location.hash = 'service/' + svc.name;
}

// openMailpitMessage opens the mailpit dashboard overlay with the iframe
// pointed at /view/<id> so a clicked email notification lands the user on
// the captured message instead of mailpit's inbox.
export function openMailpitMessage(id: string) {
  const mp = get(services).find((s) => s.name === 'mailpit');
  if (!mp?.dashboard) return;
  const safeId = encodeURIComponent(id);
  dashboardOpen.set({
    name: 'mailpit',
    label: 'Mailpit',
    dashboard: mp.dashboard,
    extraPath: '/view/' + safeId
  });
  location.hash = 'service/mailpit/view/' + safeId;
}

export function openDocs() {
  const cur = get(dashboardOpen);
  if (cur && cur.name === 'docs') {
    dashboardOpen.set(null);
    location.hash = fallbackHash();
    return;
  }
  dashboardOpen.set(DOCS_REF);
  location.hash = 'docs';
}

export function closeDashboard() {
  dashboardOpen.set(null);
  location.hash = fallbackHash();
}

// Services eligible for an iframe dashboard entry (active + has dashboard + not external-only).
export const dashboardServices = derived(services, ($s) =>
  $s.filter((x) => x.status === 'active' && x.dashboard && !x.dashboard_external)
);

function refFromHash(): DashboardRef | null {
  const h = location.hash.slice(1);
  if (h === 'docs') return DOCS_REF;
  if (h.startsWith('service/')) {
    const rest = h.slice('service/'.length);
    // service/mailpit/view/<id> deep-links into a specific captured email.
    const mpDeep = rest.match(/^mailpit\/view\/(.+)$/);
    if (mpDeep) {
      const mp = get(services).find((x) => x.name === 'mailpit');
      if (mp?.dashboard) {
        return {
          name: 'mailpit',
          label: 'Mailpit',
          dashboard: mp.dashboard,
          extraPath: '/view/' + mpDeep[1]
        };
      }
    }
    const svc = get(services).find((x) => x.name === rest);
    if (svc?.dashboard) return { name: svc.name, label: svc.name, dashboard: svc.dashboard };
  }
  return null;
}

export function initDashboardRoute() {
  dashboardOpen.set(refFromHash());
  window.addEventListener('hashchange', () => {
    dashboardOpen.set(refFromHash());
  });
  // Re-hydrate when services load so a #service/<name> deep-link resolves.
  services.subscribe(() => {
    const h = location.hash.slice(1);
    if (h.startsWith('service/') || h === 'docs') {
      dashboardOpen.set(refFromHash());
    }
  });
}
