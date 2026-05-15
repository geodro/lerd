import { describe, it, expect, beforeEach } from 'vitest';
import { get } from 'svelte/store';

describe('dashboard store', () => {
  beforeEach(() => {
    location.hash = '';
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
