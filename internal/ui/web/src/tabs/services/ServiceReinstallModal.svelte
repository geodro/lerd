<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import type { Service } from '$stores/services';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    open: boolean;
    svc: Service;
    onclose: () => void;
    onconfirm: (opts: { resetData: boolean }) => void | Promise<void>;
  }
  let { open, svc, onclose, onconfirm }: Props = $props();

  let resetData = $state(false);
  let typedName = $state('');
  let submitting = $state(false);

  const dependents = $derived(svc.site_count || 0);
  // Type-the-name confirmation only fires for the destructive combo:
  // resetting data on a default service that linked sites depend on. Mirrors
  // ServiceDeleteModal's gating so the friction is consistent.
  const requiresTypedConfirm = $derived(resetData && Boolean(svc.is_default) && dependents > 0);
  const canConfirm = $derived(!submitting && (!requiresTypedConfirm || typedName.trim() === svc.name));

  $effect(() => {
    if (open) {
      resetData = false;
      typedName = '';
      submitting = false;
    }
  });

  async function confirm() {
    if (!canConfirm) return;
    submitting = true;
    try {
      await onconfirm({ resetData });
    } finally {
      submitting = false;
      onclose();
    }
  }
</script>

<Modal {open} {onclose} title={m.services_reinstall_title({ name: svc.name })} size="sm">
  <div class="px-5 py-4 space-y-3">
    <p class="text-sm text-gray-600 dark:text-gray-400">
      {m.services_reinstall_body({ name: svc.name })}
    </p>

    <label class="flex items-start gap-2 text-sm text-gray-700 dark:text-gray-300 cursor-pointer">
      <input
        type="checkbox"
        bind:checked={resetData}
        class="mt-0.5 w-4 h-4 rounded-sm border-gray-300 dark:border-lerd-border bg-white dark:bg-lerd-bg text-lerd-red focus:ring-lerd-red/40 cursor-pointer"
      />
      <span>
        {m.services_reinstall_resetLabel()}
        <span class="block text-[11px] text-gray-500 dark:text-gray-400">
          {m.services_reinstall_resetHint({ name: svc.name, count: dependents })}
        </span>
      </span>
    </label>

    {#if resetData && dependents > 0}
      <div class="bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-900/40 rounded-sm px-3 py-2 text-xs text-amber-700 dark:text-amber-300">
        {m.services_reinstall_wipeWarn({ count: dependents })}
      </div>
    {/if}

    {#if requiresTypedConfirm}
      <div class="space-y-1">
        <label for="reinstall-confirm-name" class="text-xs text-gray-600 dark:text-gray-400">
          {m.services_confirm_typeBefore()} <span class="font-mono font-medium text-gray-800 dark:text-gray-200">{svc.name}</span> {m.services_confirm_typeAfter()}
        </label>
        <input
          id="reinstall-confirm-name"
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
      class="text-xs px-3 py-1.5 rounded-sm {resetData ? 'bg-lerd-red hover:bg-lerd-redhov' : 'bg-lerd-red/80 hover:bg-lerd-red'} text-white transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
    >{submitting ? m.services_reinstall_submitting() : resetData ? m.services_reinstall_withReset() : m.services_reinstall_action()}</button>
  {/snippet}
</Modal>
