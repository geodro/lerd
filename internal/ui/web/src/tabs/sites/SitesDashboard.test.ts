import { render } from '@testing-library/svelte';
import { describe, it, expect, beforeEach } from 'vitest';
import SitesDashboard from './SitesDashboard.svelte';
import { sites, sitesLoaded, type Site } from '$stores/sites';

function site(over: Partial<Site> = {}): Site {
  return { domain: 'app.test', ...over } as Site;
}

describe('SitesDashboard', () => {
  beforeEach(() => {
    sites.set([]);
    sitesLoaded.set(true);
  });

  it('groups active sites under their framework label', () => {
    sites.set([
      site({ domain: 'shop.test', framework_label: 'Laravel', php_version: '8.3', fpm_running: true }),
      site({ domain: 'blog.test', framework_label: 'WordPress', php_version: '8.2' })
    ]);
    const { getByText } = render(SitesDashboard);
    expect(getByText('Laravel')).toBeTruthy();
    expect(getByText('WordPress')).toBeTruthy();
    expect(getByText('shop.test')).toBeTruthy();
    expect(getByText('blog.test')).toBeTruthy();
  });

  it('buckets sites without a framework under Other', () => {
    sites.set([site({ domain: 'static.test' })]);
    const { getByText } = render(SitesDashboard);
    expect(getByText('Other')).toBeTruthy();
  });

  it('lists paused sites in their own section', () => {
    sites.set([site({ domain: 'old.test', paused: true })]);
    const { getByText } = render(SitesDashboard);
    expect(getByText('Paused')).toBeTruthy();
    expect(getByText('old.test')).toBeTruthy();
  });

  it('shows the empty hint when there are no sites', () => {
    sites.set([]);
    const { getByText } = render(SitesDashboard);
    expect(getByText('No sites yet')).toBeTruthy();
  });
});
