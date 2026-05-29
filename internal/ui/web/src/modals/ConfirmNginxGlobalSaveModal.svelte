<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import { closeModal, modal } from '$stores/modals';
  import { saveNginxConfig } from '$stores/nginx';
  import { m } from '../paraglide/messages.js';

  const target = $derived($modal.nginxGlobalSave);

  let backup = $state(false);
  let busy = $state(false);
  let error = $state('');
  let validationOutput = $state('');

  function safeClose() {
    if (busy) return;
    closeModal();
  }

  async function confirm() {
    if (!target) return;
    const onSuccess = $modal.onSuccess;
    busy = true;
    error = '';
    validationOutput = '';
    try {
      const res = await saveNginxConfig(target.content, backup);
      if (!res.ok) {
        error = res.error || m.nginxEditor_saveFailed();
        validationOutput = res.validationOutput || '';
        return;
      }
      closeModal();
      try {
        await onSuccess?.();
      } catch {
        /* surfaced in tab state by the caller's refreshAfterAction */
      }
    } catch (e: unknown) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }
</script>

<Modal open title={m.nginxEditor_confirmTitle()} onclose={safeClose} size="md">
  <div class="px-5 py-4 space-y-3">
    {#if !target}
      <p class="text-sm text-gray-500 dark:text-gray-400">{m.common_loading()}</p>
    {:else}
      <p class="text-sm text-gray-700 dark:text-gray-300">
        {m.nginxGlobalEditor_confirmBody()}
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
            {m.nginxEditor_backupLabel()}
            <span class="block text-[10px] text-gray-400 mt-0.5 font-mono">zz-lerd-user.conf.bkp.&lt;YYYYMMDD-HHMMSS&gt;</span>
          </span>
        </label>
      {/if}

      {#if error}
        <p class="text-xs text-red-500">{error}</p>
      {/if}
      {#if validationOutput}
        <pre class="text-[11px] leading-relaxed font-mono p-3 rounded-sm border border-red-200 dark:border-red-900/40 bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-300 overflow-auto max-h-48 whitespace-pre-wrap">{validationOutput}</pre>
      {/if}
    {/if}
  </div>

  {#snippet footer()}
    <DetailButton onclick={safeClose} disabled={busy}>{m.common_cancel()}</DetailButton>
    {#if target}
      <DetailButton tone="primary" onclick={confirm} loading={busy} disabled={busy}>
        {busy ? m.nginxEditor_saving() : m.common_save()}
      </DetailButton>
    {/if}
  {/snippet}
</Modal>
