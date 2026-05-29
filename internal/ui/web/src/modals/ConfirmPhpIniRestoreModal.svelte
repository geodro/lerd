<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import { closeModal, modal } from '$stores/modals';
  import { restorePhpIni } from '$stores/phpVersions';
  import { diffLines } from '$lib/diff';
  import { m } from '../paraglide/messages.js';

  const target = $derived($modal.phpIniRestore);

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
      const res = await restorePhpIni(target.version, target.backupName);
      if (!res.ok) {
        error = res.error || m.nginxEditor_restoreFailed();
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

<Modal open title={m.phpIniEditor_restoreTitle()} onclose={safeClose} size="lg">
  <div class="px-5 py-4 space-y-3">
    {#if !target}
      <p class="text-sm text-gray-500 dark:text-gray-400">{m.common_loading()}</p>
    {:else}
      <p class="text-sm text-gray-700 dark:text-gray-300">
        {m.phpIniEditor_restoreBody({ version: target.version, name: target.backupName })}
      </p>

      <div class="border border-gray-200 dark:border-lerd-border rounded-sm bg-gray-50 dark:bg-black/40 overflow-auto max-h-96">
        {#if lines.length === 0}
          <p class="text-xs text-gray-400 px-3 py-2">{m.nginxEditor_restoreNoDiff()}</p>
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
        {busy ? m.nginxEditor_restoring() : m.nginxEditor_restoreAccept()}
      </DetailButton>
    {/if}
  {/snippet}
</Modal>
