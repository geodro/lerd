<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import { closeModal, modal } from '$stores/modals';
  import { saveServiceConfig } from '$stores/services';
  import { m } from '../paraglide/messages.js';

  const target = $derived($modal.tuningSave);

  let backup = $state(false);
  let busy = $state(false);
  let error = $state('');

  function safeClose() {
    if (busy) return;
    closeModal();
  }

  async function confirm() {
    if (!target) return;
    // Snapshot the success callback before the await so a concurrent
    // modal-store mutation cannot clear $modal mid-flight and drop
    // the post-save refresh, leaving the editor with stale dirty /
    // backup state.
    const onSuccess = $modal.onSuccess;
    busy = true;
    error = '';
    try {
      const res = await saveServiceConfig(target.name, target.content, backup);
      if (!res.ok) {
        // Surface the error and leave the modal open so the user can
        // read the diagnostic and either fix and retry or cancel.
        //
        // IMPORTANT: do NOT call onSuccess() here even when
        // res.rolledBack is true. onSuccess refreshes the editor's
        // `original` and `text` baselines from the server, which
        // would replace whatever the user typed with the rolled-back
        // bytes — silently discarding their work the moment the
        // modal stays open showing the failure. The editor's `text`
        // already holds what they tried to save; leaving it untouched
        // means the dirty indicator and a re-save still target their
        // intended changes.
        error = res.error || m.tuningEditor_saveFailed();
        return;
      }
      closeModal();
      try {
        await onSuccess?.();
      } catch {
        /* surfaced in tab state by the caller */
      }
    } catch (e: unknown) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }
</script>

<Modal open title={m.tuningEditor_confirmTitle()} onclose={safeClose} size="md">
  <div class="px-5 py-4 space-y-3">
    {#if !target}
      <p class="text-sm text-gray-500 dark:text-gray-400">{m.common_loading()}</p>
    {:else}
      <p class="text-sm text-gray-700 dark:text-gray-300">
        {m.tuningEditor_confirmBody({ name: target.name })}
      </p>

      {#if target.exists}
        <label class="flex items-start gap-2 text-xs text-gray-700 dark:text-gray-300">
          <input
            type="checkbox"
            bind:checked={backup}
            disabled={busy}
            class="mt-0.5 rounded-sm border-gray-300 dark:border-lerd-border"
          />
          <span>
            {m.tuningEditor_backupLabel()}
            <span class="block text-[10px] text-gray-400 mt-0.5 font-mono">{target.name}.conf.bkp.&lt;YYYYMMDD-HHMMSS&gt;</span>
          </span>
        </label>
      {/if}

      {#if error}
        <p class="text-xs text-red-500">{error}</p>
      {/if}
    {/if}
  </div>

  {#snippet footer()}
    <DetailButton onclick={safeClose} disabled={busy}>{m.common_cancel()}</DetailButton>
    {#if target}
      <DetailButton tone="primary" onclick={confirm} loading={busy} disabled={busy}>
        {busy ? m.tuningEditor_saving() : m.common_save()}
      </DetailButton>
    {/if}
  {/snippet}
</Modal>
