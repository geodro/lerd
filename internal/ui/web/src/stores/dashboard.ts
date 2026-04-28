import { writable, derived, get } from 'svelte/store';
import { services, type Service } from './services';

export interface DashboardRef {
  name: string;
  label?: string;
  dashboard: string;
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
    const name = h.slice('service/'.length);
    const svc = get(services).find((x) => x.name === name);
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
