<script lang="ts">
  import { modal } from '$stores/modals';
  import DomainModal from './DomainModal.svelte';
  import LinkModal from './LinkModal.svelte';
  import PresetModal from './PresetModal.svelte';
  import RemoteControlModal from './RemoteControlModal.svelte';
  import LANProgressModal from './LANProgressModal.svelte';
  import WorktreeModal from './WorktreeModal.svelte';
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
{:else if $modal.kind === 'worktree' && $modal.site}
  <WorktreeModal site={$modal.site} />
{/if}
