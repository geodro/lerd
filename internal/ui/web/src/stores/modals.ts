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
  | 'phpAdd'
  | 'envSave'
  | 'envRestore'
  | 'nginxSave'
  | 'nginxRestore'
  | 'nginxReset'
  | 'nginxGlobalSave'
  | 'nginxGlobalRestore'
  | 'nginxGlobalReset'
  | 'phpIniSave'
  | 'phpIniRestore'
  | 'phpIniReset'
  | 'phpRemove'
  | 'tuningSave'
  | 'tuningRestore'
  | 'tuningReset'
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

export interface NginxSaveTarget {
  domain: string;
  content: string;
  original: string;
  /** True when the live override already exists on disk. When false, the
   *  save modal hides the "back up the current file first" checkbox since
   *  there's nothing on disk worth preserving. */
  exists: boolean;
}

export interface NginxRestoreTarget {
  domain: string;
  current: string;
  backupName: string;
  backup: string;
}

export interface NginxResetTarget {
  domain: string;
  path: string;
}

export interface NginxGlobalSaveTarget {
  content: string;
  original: string;
  exists: boolean;
}

export interface NginxGlobalRestoreTarget {
  current: string;
  backupName: string;
  backup: string;
}

export interface NginxGlobalResetTarget {
  path: string;
}

export interface PhpIniSaveTarget {
  version: string;
  content: string;
  original: string;
  exists: boolean;
}

export interface PhpIniRestoreTarget {
  version: string;
  current: string;
  backupName: string;
  backup: string;
}

export interface PhpIniResetTarget {
  version: string;
  path: string;
}

export interface PhpRemoveTarget {
  version: string;
  siteCount: number;
}

export interface TuningSaveTarget {
  name: string;
  content: string;
  original: string;
  /** True when the live override already exists on disk; the save
   *  modal hides the back-up-first checkbox when false because there
   *  is nothing on disk yet to protect. */
  exists: boolean;
}

export interface TuningRestoreTarget {
  name: string;
  current: string;
  backupName: string;
  backup: string;
}

export interface TuningResetTarget {
  name: string;
  path: string;
}

export interface ModalState {
  kind: ModalKind;
  site?: Site;
  lanAction?: LANAction;
  onSuccess?: () => void;
  branch?: string;
  envSave?: EnvSaveTarget;
  envRestore?: EnvRestoreTarget;
  nginxSave?: NginxSaveTarget;
  nginxRestore?: NginxRestoreTarget;
  nginxReset?: NginxResetTarget;
  nginxGlobalSave?: NginxGlobalSaveTarget;
  nginxGlobalRestore?: NginxGlobalRestoreTarget;
  nginxGlobalReset?: NginxGlobalResetTarget;
  phpIniSave?: PhpIniSaveTarget;
  phpIniRestore?: PhpIniRestoreTarget;
  phpIniReset?: PhpIniResetTarget;
  phpRemove?: PhpRemoveTarget;
  tuningSave?: TuningSaveTarget;
  tuningRestore?: TuningRestoreTarget;
  tuningReset?: TuningResetTarget;
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

export function openPhpAddModal() {
  modal.set({ kind: 'phpAdd' });
}

export function openEnvSaveModal(target: EnvSaveTarget, onSuccess?: () => void) {
  modal.set({ kind: 'envSave', envSave: target, onSuccess });
}

export function openEnvRestoreModal(target: EnvRestoreTarget, onSuccess?: () => void) {
  modal.set({ kind: 'envRestore', envRestore: target, onSuccess });
}

export function openNginxSaveModal(target: NginxSaveTarget, onSuccess?: () => void) {
  modal.set({ kind: 'nginxSave', nginxSave: target, onSuccess });
}

export function openNginxRestoreModal(target: NginxRestoreTarget, onSuccess?: () => void) {
  modal.set({ kind: 'nginxRestore', nginxRestore: target, onSuccess });
}

export function openNginxResetModal(target: NginxResetTarget, onSuccess?: () => void) {
  modal.set({ kind: 'nginxReset', nginxReset: target, onSuccess });
}

export function openNginxGlobalSaveModal(target: NginxGlobalSaveTarget, onSuccess?: () => void) {
  modal.set({ kind: 'nginxGlobalSave', nginxGlobalSave: target, onSuccess });
}

export function openNginxGlobalRestoreModal(target: NginxGlobalRestoreTarget, onSuccess?: () => void) {
  modal.set({ kind: 'nginxGlobalRestore', nginxGlobalRestore: target, onSuccess });
}

export function openNginxGlobalResetModal(target: NginxGlobalResetTarget, onSuccess?: () => void) {
  modal.set({ kind: 'nginxGlobalReset', nginxGlobalReset: target, onSuccess });
}

export function openPhpIniSaveModal(target: PhpIniSaveTarget, onSuccess?: () => void) {
  modal.set({ kind: 'phpIniSave', phpIniSave: target, onSuccess });
}

export function openPhpIniRestoreModal(target: PhpIniRestoreTarget, onSuccess?: () => void) {
  modal.set({ kind: 'phpIniRestore', phpIniRestore: target, onSuccess });
}

export function openPhpIniResetModal(target: PhpIniResetTarget, onSuccess?: () => void) {
  modal.set({ kind: 'phpIniReset', phpIniReset: target, onSuccess });
}

export function openPhpRemoveModal(target: PhpRemoveTarget) {
  modal.set({ kind: 'phpRemove', phpRemove: target });
}

export function openTuningSaveModal(target: TuningSaveTarget, onSuccess?: () => void) {
  modal.set({ kind: 'tuningSave', tuningSave: target, onSuccess });
}

export function openTuningRestoreModal(target: TuningRestoreTarget, onSuccess?: () => void) {
  modal.set({ kind: 'tuningRestore', tuningRestore: target, onSuccess });
}

export function openTuningResetModal(target: TuningResetTarget, onSuccess?: () => void) {
  modal.set({ kind: 'tuningReset', tuningReset: target, onSuccess });
}

export function closeModal() {
  modal.set({ kind: null });
}
