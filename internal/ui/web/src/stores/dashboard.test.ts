import { describe, it, expect, beforeEach, vi } from 'vitest';
import { get } from 'svelte/store';

describe('dashboard store', () => {
  beforeEach(() => {
    location.hash = '';
  });

  it('proxied bundled dashboard embeds in the overlay and lists in the sidebar', async () => {
    const { services } = await import('./services');
    const { openDashboard, dashboardOpen, dashboardServices } = await import('./dashboard');

    const rabbit = {
      name: 'rabbitmq',
      status: 'active',
      site_count: 0,
      dashboard: '/_svc/rabbitmq/',
      dashboard_external: false
    };
    services.set([rabbit]);

    expect(get(dashboardServices).some((s) => s.name === 'rabbitmq')).toBe(true);

    const open = vi.spyOn(window, 'open').mockImplementation(() => null);
    openDashboard(rabbit);
    expect(open).not.toHaveBeenCalled();
    expect(get(dashboardOpen)?.dashboard).toBe('/_svc/rabbitmq/');
    expect(location.hash).toBe('#service/rabbitmq');
    open.mockRestore();
  });

  it('user external dashboard still opens in a new tab and is not embedded', async () => {
    const { services } = await import('./services');
    const { openDashboard, dashboardOpen, dashboardServices } = await import('./dashboard');

    const ext = {
      name: 'myadmin',
      status: 'active',
      site_count: 0,
      dashboard: 'http://localhost:9000',
      dashboard_external: true
    };
    services.set([ext]);
    dashboardOpen.set(null);

    expect(get(dashboardServices).some((s) => s.name === 'myadmin')).toBe(false);

    const open = vi.spyOn(window, 'open').mockImplementation(() => null);
    openDashboard(ext);
    expect(open).toHaveBeenCalledWith('http://localhost:9000', '_blank', 'noopener,noreferrer');
    expect(get(dashboardOpen)).toBeNull();
    open.mockRestore();
  });

  it('openDocs embeds the docs landing page in the overlay', async () => {
    const { openDocs, dashboardOpen } = await import('./dashboard');

    dashboardOpen.set(null);
    openDocs();
    const cur = get(dashboardOpen);
    expect(cur?.name).toBe('docs');
    expect(cur?.dashboard).toBe('https://lerd.sh/getting-started/requirements');
    expect(location.hash).toBe('#docs');
  });

  it('openMailpitMessage opens overlay with extraPath when mailpit is present', async () => {
    const { services } = await import('./services');
    const { openMailpitMessage, dashboardOpen } = await import('./dashboard');

    services.set([
      { name: 'mailpit', status: 'active', site_count: 0, dashboard: 'http://localhost:8025' }
    ]);

    openMailpitMessage('abc123');
    const cur = get(dashboardOpen);
    expect(cur?.name).toBe('mailpit');
    expect(cur?.dashboard).toBe('http://localhost:8025');
    expect(cur?.extraPath).toBe('/view/abc123');
    expect(location.hash).toBe('#service/mailpit/view/abc123');
  });

  it('openMailpitMessage is a no-op when mailpit has no dashboard', async () => {
    const { services } = await import('./services');
    const { openMailpitMessage, dashboardOpen } = await import('./dashboard');

    services.set([{ name: 'mailpit', status: 'inactive', site_count: 0 }]);
    dashboardOpen.set(null);

    openMailpitMessage('abc');
    expect(get(dashboardOpen)).toBeNull();
  });

  it('encodes ids that contain url-special characters', async () => {
    const { services } = await import('./services');
    const { openMailpitMessage, dashboardOpen } = await import('./dashboard');

    services.set([
      { name: 'mailpit', status: 'active', site_count: 0, dashboard: 'http://localhost:8025' }
    ]);

    openMailpitMessage('id/with spaces');
    const cur = get(dashboardOpen);
    expect(cur?.extraPath).toBe('/view/id%2Fwith%20spaces');
  });
});
