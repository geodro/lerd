<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import { closeModal, modal } from '$stores/modals';
  import { restoreServiceTuning } from '$stores/services';
  import { diffLines } from '$lib/diff';
  import { m } from '../paraglide/messages.js';

  const target = $derived($modal.tuningRestore);

  const lines = $derived.by(() => {
    if (!target) return [];
    return diffLines(target.current, target.backup);
  });

  let busy = $state(false);
  let error = $state('');

  function safeClose() {
    if (busy) return;
    closeModal();
  }

  async function accept() {
    if (!target) return;
    const onSuccess = $modal.onSuccess;
    busy = true;
    error = '';
    try {
      // Pass the exact backup name previewed in this diff so a
      // concurrent save landing a newer backup between modal-open and
      // accept cannot silently swap a different file into place.
      const res = await restoreServiceTuning(target.name, target.backupName);
      if (!res.ok) {
        error = res.error || m.tuningEditor_restoreFailed();
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

<Modal open title={m.tuningEditor_restoreTitle()} onclose={safeClose} size="lg">
  <div class="px-5 py-4 space-y-3">
    {#if !target}
      <p class="text-sm text-gray-500 dark:text-gray-400">{m.common_loading()}</p>
    {:else}
      <p class="text-sm text-gray-700 dark:text-gray-300">
        {m.tuningEditor_restoreBody({ name: target.name, backup: target.backupName })}
      </p>

      <div class="border border-gray-200 dark:border-lerd-border rounded-sm bg-gray-50 dark:bg-black/40 overflow-auto max-h-96">
        {#if lines.length === 0}
          <p class="text-xs text-gray-400 px-3 py-2">{m.tuningEditor_restoreNoDiff()}</p>
        {:else}
          <pre class="text-[11px] leading-relaxed font-mono"><!--
            -->{#each lines as l, i (i)}<!--
              -->{#if l.op === '+'}<!--
                --><span class="block px-3 bg-emerald-50 dark:bg-emerald-900/20 text-emerald-700 dark:text-emerald-300">+ {l.line}</span><!--
              -->{:else if l.op === '-'}<!--
                --><span class="block px-3 bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-300">- {l.line}</span><!--
              -->{:else}<!--
                --><span class="block px-3 text-gray-600 dark:text-gray-400">  {l.line}</span><!--
              -->{/if}<!--
            -->{/each}<!--
          --></pre>
        {/if}
      </div>

      {#if error}
        <p class="text-xs text-red-500">{error}</p>
      {/if}
    {/if}
  </div>

  {#snippet footer()}
    <DetailButton onclick={safeClose} disabled={busy}>{m.common_cancel()}</DetailButton>
    {#if target}
      <DetailButton tone="primary" onclick={accept} loading={busy} disabled={busy}>
        {busy ? m.tuningEditor_restoring() : m.tuningEditor_restoreAccept()}
      </DetailButton>
    {/if}
  {/snippet}
</Modal>
