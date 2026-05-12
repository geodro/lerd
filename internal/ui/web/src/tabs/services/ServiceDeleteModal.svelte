<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import type { Service } from '$stores/services';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    open: boolean;
    svc: Service;
    onclose: () => void;
    onconfirm: (opts: { removeData: boolean }) => void | Promise<void>;
  }
  let { open, svc, onclose, onconfirm }: Props = $props();

  let removeData = $state(false);
  let typedName = $state('');
  let submitting = $state(false);

  const dependents = $derived(svc.site_count || 0);
  const requiresTypedConfirm = $derived(Boolean(svc.is_default) && dependents > 0);
  const canConfirm = $derived(!submitting && (!requiresTypedConfirm || typedName.trim() === svc.name));

  $effect(() => {
    if (open) {
      removeData = false;
      typedName = '';
      submitting = false;
    }
  });

  async function confirm() {
    if (!canConfirm) return;
    submitting = true;
    try {
      await onconfirm({ removeData });
    } finally {
      submitting = false;
      onclose();
    }
  }
</script>

<Modal {open} {onclose} title={m.services_delete_title({ name: svc.name })} size="sm">
  <div class="px-5 py-4 space-y-3">
    {#if requiresTypedConfirm}
      <div class="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-900/40 rounded-sm px-3 py-2 text-xs text-red-700 dark:text-red-300">
        <p class="font-medium mb-1">{m.services_delete_dependentsWarn({ count: dependents })}</p>
        {#if svc.site_domains && svc.site_domains.length > 0}
          <ul class="list-disc list-inside space-y-0.5">
            {#each svc.site_domains as d (d)}
              <li class="font-mono">{d}</li>
            {/each}
          </ul>
        {/if}
        <p class="mt-2">{m.services_delete_breakWarn()}</p>
      </div>
    {/if}

    <p class="text-sm text-gray-600 dark:text-gray-400">
      {m.services_delete_body()}
    </p>

    <label class="flex items-start gap-2 text-sm text-gray-700 dark:text-gray-300 cursor-pointer">
      <input
        type="checkbox"
        bind:checked={removeData}
        class="mt-0.5 w-4 h-4 rounded-sm border-gray-300 dark:border-lerd-border bg-white dark:bg-lerd-bg text-lerd-red focus:ring-lerd-red/40 cursor-pointer"
      />
      <span>
        {m.services_delete_removeDataLabel()}
        <span class="block text-[11px] text-gray-500 dark:text-gray-400">
          {m.services_delete_removeDataHint({ name: svc.name })}
        </span>
      </span>
    </label>

    {#if requiresTypedConfirm}
      <div class="space-y-1">
        <label for="confirm-name" class="text-xs text-gray-600 dark:text-gray-400">
          {m.services_confirm_typeBefore()} <span class="font-mono font-medium text-gray-800 dark:text-gray-200">{svc.name}</span> {m.services_confirm_typeAfter()}
        </label>
        <input
          id="confirm-name"
          type="text"
          bind:value={typedName}
          class="w-full text-sm bg-white dark:bg-lerd-bg border border-gray-200 dark:border-lerd-border rounded-sm px-2.5 py-1.5 text-gray-700 dark:text-gray-300 focus:outline-hidden focus:border-lerd-red/50"
          autocomplete="off"
        />
      </div>
    {/if}
  </div>
  {#snippet footer()}
    <button
      type="button"
      onclick={onclose}
      class="text-xs px-3 py-1.5 rounded-sm border border-gray-200 dark:border-lerd-border text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-white/5 transition-colors"
    >{m.common_cancel()}</button>
    <button
      type="button"
      onclick={confirm}
      disabled={!canConfirm}
      class="text-xs px-3 py-1.5 rounded-sm bg-lerd-red hover:bg-lerd-redhov text-white transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
    >{submitting ? m.services_delete_submitting() : m.common_remove()}</button>
  {/snippet}
</Modal>
