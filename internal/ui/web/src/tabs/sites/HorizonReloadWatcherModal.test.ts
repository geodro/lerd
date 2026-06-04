import { render, screen, fireEvent } from '@testing-library/svelte';
import { describe, it, expect, vi, beforeEach } from 'vitest';

const { installHorizonReloadWatcher } = vi.hoisted(() => ({
  installHorizonReloadWatcher: vi.fn()
}));
vi.mock('$stores/sites', () => ({ installHorizonReloadWatcher }));

import Harness from './HorizonReloadWatcherModal.test.svelte';

const site = { domain: 'acme.test' } as never;

describe('HorizonReloadWatcherModal', () => {
  beforeEach(() => installHorizonReloadWatcher.mockReset());

  it('renders the title and install action when open', () => {
    render(Harness, { props: { open: true, site } });
    expect(screen.getByText('Enable Horizon auto-reload')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Install chokidar' })).toBeInTheDocument();
  });

  it('installs, then signals installed and closes on success', async () => {
    installHorizonReloadWatcher.mockResolvedValue({ ok: true });
    const oninstalled = vi.fn();
    const onclose = vi.fn();
    render(Harness, { props: { open: true, site, oninstalled, onclose } });
    await fireEvent.click(screen.getByRole('button', { name: 'Install chokidar' }));
    expect(installHorizonReloadWatcher).toHaveBeenCalledOnce();
    expect(oninstalled).toHaveBeenCalledOnce();
    expect(onclose).toHaveBeenCalledOnce();
  });

  it('does not enable reload if the modal is dismissed mid-install', async () => {
    let resolveInstall: (v: { ok: boolean }) => void = () => {};
    installHorizonReloadWatcher.mockReturnValue(
      new Promise((r) => {
        resolveInstall = r;
      })
    );
    const oninstalled = vi.fn();
    const onclose = vi.fn();
    const { rerender } = render(Harness, { props: { open: true, site, oninstalled, onclose } });
    await fireEvent.click(screen.getByRole('button', { name: 'Install chokidar' }));
    // User dismisses (Escape/backdrop) while npm is still running.
    await rerender({ open: false, site, oninstalled, onclose });
    resolveInstall({ ok: true });
    await new Promise((r) => setTimeout(r, 0));
    expect(oninstalled).not.toHaveBeenCalled();
  });

  it('shows the error and stays open on failure', async () => {
    installHorizonReloadWatcher.mockResolvedValue({ ok: false, error: 'npm boom' });
    const oninstalled = vi.fn();
    const onclose = vi.fn();
    render(Harness, { props: { open: true, site, oninstalled, onclose } });
    await fireEvent.click(screen.getByRole('button', { name: 'Install chokidar' }));
    expect(await screen.findByText('npm boom')).toBeInTheDocument();
    expect(oninstalled).not.toHaveBeenCalled();
    expect(onclose).not.toHaveBeenCalled();
  });
});
