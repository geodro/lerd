<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import { closeModal, modal } from '$stores/modals';
  import { removePhp, loadPhpVersions } from '$stores/phpVersions';
  import { loadStatus } from '$stores/status';
  import { m } from '../paraglide/messages.js';

  const target = $derived($modal.phpRemove);

  let busy = $state(false);
  let error = $state('');

  function safeClose() {
    if (busy) return;
    closeModal();
  }

  async function confirm() {
    if (!target) return;
    busy = true;
    error = '';
    try {
      const ok = await removePhp(target.version);
      if (!ok) {
        error = m.common_failed();
        return;
      }
      // Refresh both the installed-version list (drives the tabs) and status so
      // the removed version disappears from the UI immediately.
      await loadPhpVersions();
      await loadStatus();
      closeModal();
    } catch (e) {
      error = e instanceof Error ? e.message : m.common_failed();
    } finally {
      busy = false;
    }
  }
</script>

<Modal open title={target ? m.system_php_removeConfirmTitle({ version: target.version }) : ''} onclose={safeClose} size="md">
  <div class="px-5 py-4 space-y-3">
    {#if !target}
      <p class="text-sm text-gray-500 dark:text-gray-400">{m.common_loading()}</p>
    {:else}
      <p class="text-sm text-gray-700 dark:text-gray-300">{m.system_php_removeConfirmBody()}</p>
      {#if target.siteCount > 0}
        <div class="text-xs font-medium text-amber-700 dark:text-amber-300 bg-amber-50 dark:bg-amber-500/10 border border-amber-200 dark:border-amber-500/30 rounded-lg px-3 py-2">
          {m.system_php_removeWarn({ count: target.siteCount })}
        </div>
      {/if}
      {#if error}
        <p class="text-xs text-red-500">{error}</p>
      {/if}
    {/if}
  </div>

  {#snippet footer()}
    <DetailButton onclick={safeClose} disabled={busy}>{m.common_cancel()}</DetailButton>
    {#if target}
      <DetailButton tone="danger" onclick={confirm} loading={busy} disabled={busy}>
        {busy ? m.system_php_removing() : m.common_remove()}
      </DetailButton>
    {/if}
  {/snippet}
</Modal>
