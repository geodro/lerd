<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import { closeModal, modal } from '$stores/modals';
  import { lan } from '$stores/lan';
  import { m } from '../paraglide/messages.js';

  const action = $derived($modal.lanAction === 'unexpose' ? 'unexpose' : 'expose');
  const title = $derived(action === 'expose' ? m.system_lan_progress_exposing() : m.system_lan_progress_stopping());
  const done = $derived(!$lan.loading);
  const error = $derived($lan.error);
</script>

<Modal open title={done ? (error ? m.common_failed() : m.common_done()) : title} onclose={closeModal}>
  <div class="px-5 py-4">
    <ul class="text-[11px] font-mono text-gray-600 dark:text-gray-400 bg-gray-50 dark:bg-white/[0.03] border border-gray-100 dark:border-lerd-border rounded-lg p-3 space-y-1 max-h-72 overflow-y-auto min-h-[4rem]">
      {#each $lan.progressSteps as line, i (i)}
        <li class="flex items-start gap-1.5">
          {#if i < $lan.progressSteps.length - 1 || done}
            <svg class="w-3 h-3 flex-shrink-0 {error && i === $lan.progressSteps.length - 1 ? 'text-red-500' : 'text-emerald-500'} mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              {#if error && i === $lan.progressSteps.length - 1}
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M6 18L18 6M6 6l12 12"/>
              {:else}
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M5 13l4 4L19 7"/>
              {/if}
            </svg>
          {:else}
            <svg class="w-3 h-3 flex-shrink-0 animate-spin text-gray-400 mt-0.5" fill="none" viewBox="0 0 24 24">
              <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"/>
              <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"/>
            </svg>
          {/if}
          <span>{line}</span>
        </li>
      {/each}
      {#if $lan.progressSteps.length === 0 && !done}
        <li class="flex items-start gap-1.5 text-gray-400 italic">
          <svg class="w-3 h-3 flex-shrink-0 animate-spin text-gray-400 mt-0.5" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"/>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"/>
          </svg>
          {m.system_lan_progress_starting()}
        </li>
      {/if}
    </ul>

    {#if error}
      <p class="text-xs text-red-500 mt-3">{error}</p>
    {/if}

    {#if done && !error}
      <p class="text-xs text-emerald-600 dark:text-emerald-500 mt-3">
        {#if action === 'expose'}
          {@html m.system_lan_progress_doneExposed({ ip: '<code class="bg-gray-100 dark:bg-white/5 px-1.5 py-0.5 rounded font-mono">' + $lan.lanIP + '</code>' })}
        {:else}
          {m.system_lan_progress_doneHidden()}
        {/if}
      </p>
    {/if}
  </div>

  {#snippet footer()}
    <DetailButton onclick={closeModal} disabled={$lan.loading}>
      {$lan.loading ? m.system_lan_progress_working() : m.common_close()}
    </DetailButton>
  {/snippet}
</Modal>
