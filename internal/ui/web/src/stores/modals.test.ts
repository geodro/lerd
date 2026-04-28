import { describe, it, expect } from 'vitest';
import { get } from 'svelte/store';
import { modal, openDomainModal, openLinkModal, openPresetModal, closeModal } from './modals';
import type { Site } from './sites';

describe('modal store', () => {
  it('defaults to closed', () => {
    closeModal();
    expect(get(modal).kind).toBeNull();
  });

  it('opens domain modal with site context', () => {
    const site = { domain: 'a.test' } as Site;
    openDomainModal(site);
    expect(get(modal).kind).toBe('domain');
    expect(get(modal).site?.domain).toBe('a.test');
    closeModal();
  });

  it('opens link modal', () => {
    openLinkModal();
    expect(get(modal).kind).toBe('link');
    closeModal();
  });

  it('opens preset modal', () => {
    openPresetModal();
    expect(get(modal).kind).toBe('preset');
    closeModal();
  });

  it('closeModal resets state', () => {
    openLinkModal();
    closeModal();
    expect(get(modal).kind).toBeNull();
  });
});
