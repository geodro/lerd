import { writable } from 'svelte/store';
import type { Site } from './sites';

export type ModalKind =
  | 'domain'
  | 'link'
  | 'preset'
  | 'remoteControl'
  | 'lanProgress'
  | 'worktreeAdd'
  | 'worktreeRemove'
  | 'envSave'
  | 'envRestore'
  | null;

export type LANAction = 'expose' | 'unexpose';

export interface EnvSaveTarget {
  domain: string;
  branch: string;
  file: string;
  content: string;
  original: string;
}

export interface EnvRestoreTarget {
  domain: string;
  branch: string;
  file: string;
  current: string;
  backupName: string;
  backup: string;
}

export interface ModalState {
  kind: ModalKind;
  site?: Site;
  lanAction?: LANAction;
  onSuccess?: () => void;
  branch?: string;
  envSave?: EnvSaveTarget;
  envRestore?: EnvRestoreTarget;
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

export function openWorktreeAddModal(site: Site) {
  modal.set({ kind: 'worktreeAdd', site });
}

export function openWorktreeRemoveModal(site: Site, branch: string) {
  modal.set({ kind: 'worktreeRemove', site, branch });
}

export function openEnvSaveModal(target: EnvSaveTarget, onSuccess?: () => void) {
  modal.set({ kind: 'envSave', envSave: target, onSuccess });
}

export function openEnvRestoreModal(target: EnvRestoreTarget, onSuccess?: () => void) {
  modal.set({ kind: 'envRestore', envRestore: target, onSuccess });
}

export function closeModal() {
  modal.set({ kind: null });
}
