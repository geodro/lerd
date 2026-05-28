<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import { closeModal, modal } from '$stores/modals';
  import { saveSiteEnv } from '$stores/sites';
  import { m } from '../paraglide/messages.js';

  const target = $derived($modal.envSave);

  let backup = $state(false);
  let busy = $state(false);
  let error = $state('');

  function safeClose() {
    if (busy) return;
    closeModal();
  }

  async function confirm() {
    if (!target) return;
    // Snapshot the success callback BEFORE awaiting; an unrelated modal
    // dispatched on the store (LAN progress, link, etc.) during the network
    // round-trip would otherwise clear $modal and the post-save refresh
    // never fires, leaving the editor with stale dirty/backup state.
    const onSuccess = $modal.onSuccess;
    busy = true;
    error = '';
    try {
      const res = await saveSiteEnv(target.domain, target.branch, target.content, backup, target.file);
      if (!res.ok) {
        error = res.error || m.envEditor_saveFailed();
        return;
      }
      closeModal();
      onSuccess?.();
    } catch (e: unknown) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }
</script>

<Modal open title={m.envEditor_confirmTitle()} onclose={safeClose} size="md">
  <div class="px-5 py-4 space-y-3">
    {#if !target}
      <p class="text-sm text-gray-500 dark:text-gray-400">{m.common_loading()}</p>
    {:else}
      <p class="text-sm text-gray-700 dark:text-gray-300">
        {m.envEditor_confirmBody({ domain: target.domain, file: target.file })}
      </p>

      <label class="flex items-start gap-2 text-xs text-gray-700 dark:text-gray-300">
        <input
          type="checkbox"
          bind:checked={backup}
          disabled={busy}
          class="mt-0.5 rounded-sm border-gray-300 dark:border-lerd-border"
        />
        <span>
          {m.envEditor_backupLabel()}
          <span class="block text-[10px] text-gray-400 mt-0.5 font-mono">{target.file}.bkp.&lt;YYYYMMDD-HHMMSS&gt;</span>
        </span>
      </label>

      {#if error}
        <p class="text-xs text-red-500">{error}</p>
      {/if}
    {/if}
  </div>

  {#snippet footer()}
    <DetailButton onclick={safeClose} disabled={busy}>{m.common_cancel()}</DetailButton>
    {#if target}
      <DetailButton tone="primary" onclick={confirm} loading={busy} disabled={busy}>
        {busy ? m.envEditor_saving() : m.common_save()}
      </DetailButton>
    {/if}
  {/snippet}
</Modal>
