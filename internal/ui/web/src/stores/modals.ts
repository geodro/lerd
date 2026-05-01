import { writable } from 'svelte/store';
import type { Site } from './sites';

export type ModalKind = 'domain' | 'link' | 'preset' | 'remoteControl' | 'lanProgress' | null;

export type LANAction = 'expose' | 'unexpose';

export interface ModalState {
  kind: ModalKind;
  site?: Site;
  lanAction?: LANAction;
  onSuccess?: () => void;
}

export const modal = writable<ModalState>({ kind: null });

export function openDomainModal(site: Site) {
  modal.set({ kind: 'domain', site });
}

export function openLinkModal() {
  modal.set({ kind: 'link' });
}

export function openPresetModal() {
  modal.set({ kind: 'preset' });
}

export function openRemoteControlModal(onSuccess?: () => void) {
  modal.set({ kind: 'remoteControl', onSuccess });
}

export function openLANProgressModal(lanAction: LANAction) {
  modal.set({ kind: 'lanProgress', lanAction });
}

export function closeModal() {
  modal.set({ kind: null });
}
