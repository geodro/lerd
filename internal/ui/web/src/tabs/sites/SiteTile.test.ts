import { render } from '@testing-library/svelte';
import { describe, it, expect } from 'vitest';
import SiteTile from './SiteTile.svelte';
import type { Site } from '$stores/sites';

function site(over: Partial<Site> = {}): Site {
  return { domain: 'app.test', ...over } as Site;
}

describe('SiteTile', () => {
  it('renders the site domain', () => {
    const { getByText } = render(SiteTile, { props: { site: site() } });
    expect(getByText('app.test')).toBeTruthy();
  });

  it('builds a subtitle from framework and php version', () => {
    const { getByText } = render(SiteTile, {
      props: { site: site({ framework_label: 'Laravel', php_version: '8.3' }) }
    });
    expect(getByText('Laravel · PHP 8.3')).toBeTruthy();
  });

  it('falls back to the node version when there is no php', () => {
    const { getByText } = render(SiteTile, {
      props: { site: site({ framework_label: 'Next.js', node_version: '20' }) }
    });
    expect(getByText('Next.js · Node 20')).toBeTruthy();
  });

  it('offers an open-in-browser button for active sites', () => {
    const { getByLabelText } = render(SiteTile, { props: { site: site() } });
    expect(getByLabelText('Open in browser')).toBeTruthy();
  });

  it('hides the open-in-browser button and dims the tile when paused', () => {
    const { container, queryByLabelText } = render(SiteTile, {
      props: { site: site({ paused: true }) }
    });
    expect(queryByLabelText('Open in browser')).toBeNull();
    expect(container.querySelector('.opacity-60')).toBeTruthy();
  });

  it('shows a running worker dot when a worker is active', () => {
    const { container } = render(SiteTile, { props: { site: site({ queue_running: true }) } });
    expect(container.querySelector('.bg-amber-400')).toBeTruthy();
  });

  it('shows a pulsing red dot when a worker is failing', () => {
    const { container } = render(SiteTile, { props: { site: site({ queue_failing: true }) } });
    expect(container.querySelector('.bg-red-500')).toBeTruthy();
  });
});
