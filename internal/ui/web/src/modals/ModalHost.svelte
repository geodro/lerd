<script lang="ts">
  import { modal } from '$stores/modals';
  import DomainModal from './DomainModal.svelte';
  import LinkModal from './LinkModal.svelte';
  import PresetModal from './PresetModal.svelte';
  import RemoteControlModal from './RemoteControlModal.svelte';
  import LANProgressModal from './LANProgressModal.svelte';
  import AddWorktreeModal from './AddWorktreeModal.svelte';
  import RemoveWorktreeModal from './RemoveWorktreeModal.svelte';
  import ConfirmEnvSaveModal from './ConfirmEnvSaveModal.svelte';
  import ConfirmEnvRestoreModal from './ConfirmEnvRestoreModal.svelte';
  import ConfirmNginxSaveModal from './ConfirmNginxSaveModal.svelte';
  import ConfirmNginxRestoreModal from './ConfirmNginxRestoreModal.svelte';
  import ConfirmNginxResetModal from './ConfirmNginxResetModal.svelte';
  import ConfirmNginxGlobalSaveModal from './ConfirmNginxGlobalSaveModal.svelte';
  import ConfirmNginxGlobalRestoreModal from './ConfirmNginxGlobalRestoreModal.svelte';
  import ConfirmNginxGlobalResetModal from './ConfirmNginxGlobalResetModal.svelte';
  import ConfirmPhpIniSaveModal from './ConfirmPhpIniSaveModal.svelte';
  import ConfirmPhpIniRestoreModal from './ConfirmPhpIniRestoreModal.svelte';
  import ConfirmPhpIniResetModal from './ConfirmPhpIniResetModal.svelte';
  import ConfirmTuningSaveModal from './ConfirmTuningSaveModal.svelte';
  import ConfirmTuningRestoreModal from './ConfirmTuningRestoreModal.svelte';
  import ConfirmTuningResetModal from './ConfirmTuningResetModal.svelte';
</script>

{#if $modal.kind === 'domain' && $modal.site}
  <DomainModal site={$modal.site} />
{:else if $modal.kind === 'link'}
  <LinkModal />
{:else if $modal.kind === 'preset'}
  <PresetModal />
{:else if $modal.kind === 'remoteControl'}
  <RemoteControlModal />
{:else if $modal.kind === 'lanProgress'}
  <LANProgressModal />
{:else if $modal.kind === 'worktreeAdd' && $modal.site}
  <AddWorktreeModal site={$modal.site} />
{:else if $modal.kind === 'worktreeRemove' && $modal.site && $modal.branch}
  <RemoveWorktreeModal site={$modal.site} branch={$modal.branch} />
{:else if $modal.kind === 'envSave' && $modal.envSave}
  <ConfirmEnvSaveModal />
{:else if $modal.kind === 'envRestore' && $modal.envRestore}
  <ConfirmEnvRestoreModal />
{:else if $modal.kind === 'nginxSave' && $modal.nginxSave}
  <ConfirmNginxSaveModal />
{:else if $modal.kind === 'nginxRestore' && $modal.nginxRestore}
  <ConfirmNginxRestoreModal />
{:else if $modal.kind === 'nginxReset' && $modal.nginxReset}
  <ConfirmNginxResetModal />
{:else if $modal.kind === 'nginxGlobalSave' && $modal.nginxGlobalSave}
  <ConfirmNginxGlobalSaveModal />
{:else if $modal.kind === 'nginxGlobalRestore' && $modal.nginxGlobalRestore}
  <ConfirmNginxGlobalRestoreModal />
{:else if $modal.kind === 'nginxGlobalReset' && $modal.nginxGlobalReset}
  <ConfirmNginxGlobalResetModal />
{:else if $modal.kind === 'phpIniSave' && $modal.phpIniSave}
  <ConfirmPhpIniSaveModal />
{:else if $modal.kind === 'phpIniRestore' && $modal.phpIniRestore}
  <ConfirmPhpIniRestoreModal />
{:else if $modal.kind === 'phpIniReset' && $modal.phpIniReset}
  <ConfirmPhpIniResetModal />
{:else if $modal.kind === 'tuningSave' && $modal.tuningSave}
  <ConfirmTuningSaveModal />
{:else if $modal.kind === 'tuningRestore' && $modal.tuningRestore}
  <ConfirmTuningRestoreModal />
{:else if $modal.kind === 'tuningReset' && $modal.tuningReset}
  <ConfirmTuningResetModal />
{/if}
