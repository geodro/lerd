import { render, screen, cleanup } from '@testing-library/svelte';
import { describe, it, expect, vi, afterEach } from 'vitest';

const loadDoctor = vi.fn();
const loadCommands = vi.fn();
const executeCommand = vi.fn();

vi.mock('$stores/doctor', () => ({
  loadDoctor: (...a: unknown[]) => loadDoctor(...a)
}));
vi.mock('$stores/commands', () => ({
  loadCommands: (...a: unknown[]) => loadCommands(...a),
  executeCommand: (...a: unknown[]) => executeCommand(...a)
}));

import SiteDoctorTab from './SiteDoctorTab.svelte';
import type { Site } from '$stores/sites';

function site(over: Partial<Site> = {}): Site {
  return { domain: 'acme.test', is_laravel: true, ...over } as Site;
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('SiteDoctorTab', () => {
  it('renders each check title and detail, with a Fix button for fixable findings', async () => {
    loadDoctor.mockResolvedValue({
      checks: [
        { name: 'app_key', status: 'fail', detail: 'APP_KEY is empty', fix: 'key:generate' },
        { name: 'app_debug', status: 'ok' }
      ],
      failures: 1,
      warnings: 0
    });
    loadCommands.mockResolvedValue([
      { name: 'key:generate', label: 'Generate APP_KEY', command: 'php artisan key:generate' }
    ]);

    render(SiteDoctorTab, { props: { site: site(), branch: '' } });

    expect(await screen.findByText('Application key')).toBeTruthy();
    expect(screen.getByText('Debug mode')).toBeTruthy();
    expect(screen.getByText('APP_KEY is empty')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Fix' })).toBeTruthy();
  });

  it('omits the Fix button when no matching command is available', async () => {
    loadDoctor.mockResolvedValue({
      checks: [{ name: 'storage_link', status: 'warn', detail: 'symlink missing', fix: 'storage:link' }],
      failures: 0,
      warnings: 1
    });
    loadCommands.mockResolvedValue([]);

    render(SiteDoctorTab, { props: { site: site(), branch: '' } });

    expect(await screen.findByText('Storage symlink')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Fix' })).toBeNull();
  });
});
