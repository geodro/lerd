<script lang="ts">
  import { onMount } from 'svelte';
  import {
    workerExecMode,
    workerModeApplies,
    workerModeLoading,
    workerModeProgress,
    loadWorkerMode,
    setWorkerMode,
    type WorkerExecMode
  } from '$stores/workerMode';
  import Modal from '$components/Modal.svelte';
  import { m } from '../../paraglide/messages.js';

  onMount(loadWorkerMode);

  let draft = $state<WorkerExecMode>($workerExecMode);
  $effect(() => {
    draft = $workerExecMode;
  });

  const dirty = $derived(draft !== $workerExecMode);

  let confirmOpen = $state(false);
  let applyError = $state('');

  function pick(mode: WorkerExecMode) {
    if ($workerModeLoading) return;
    draft = mode;
  }

  function openConfirm() {
    if (!dirty || $workerModeLoading) return;
    applyError = '';
    confirmOpen = true;
  }

  function cancelConfirm() {
    if ($workerModeLoading) return;
    confirmOpen = false;
  }

  async function applyChange() {
    applyError = '';
    const r = await setWorkerMode(draft);
    if (r.ok) {
      workerModeProgress.set(null);
      confirmOpen = false;
    } else {
      applyError = r.error
        ? m.system_workerMode_apply_failed() + ' (' + r.error + ')'
        : m.system_workerMode_apply_failed();
    }
  }

  // Human-readable progress line for the modal. Maps phase + unit + step
  // into something like "Stopping lerd-horizon-parkapp" / "Starting …".
  function progressLabel(p: { phase: string; unit?: string; step?: string; message?: string } | null): string {
    if (!p) return m.system_workerMode_apply_running();
    if (p.message) return p.message;
    switch (p.phase) {
      case 'saving_config':
        return m.system_workerMode_progress_savingConfig();
      case 'sweeping_orphans':
        return m.system_workerMode_progress_sweeping();
      case 'migrating_worker': {
        const unit = p.unit || '';
        switch (p.step) {
          case 'stopping':
            return m.system_workerMode_progress_stopping({ unit });
          case 'cleaning':
            return m.system_workerMode_progress_cleaning({ unit });
          case 'starting':
            return m.system_workerMode_progress_starting({ unit });
          default:
            return unit;
        }
      }
      default:
        return m.system_workerMode_apply_running();
    }
  }
</script>

{#if $workerModeApplies}
  <div class="flex-1 overflow-y-auto">
    <div class="flex flex-wrap items-center justify-between gap-y-2 px-3 sm:px-5 py-4 border-b border-gray-100 dark:border-lerd-border">
      <span class="font-semibold text-gray-900 dark:text-white text-base">{m.system_workerMode_title()}</span>
      <span
        class="inline-flex items-center gap-1.5 text-xs font-medium px-2.5 py-1 rounded-full {$workerExecMode === 'container'
          ? 'bg-sky-100 dark:bg-sky-500/10 text-sky-700 dark:text-sky-400'
          : 'bg-emerald-100 dark:bg-emerald-500/10 text-emerald-700 dark:text-emerald-500'}"
      >
        <span class="w-1.5 h-1.5 rounded-full {$workerExecMode === 'container' ? 'bg-sky-500' : 'bg-emerald-500'}"></span>
        {$workerExecMode === 'container' ? m.system_workerMode_containerBadge() : m.system_workerMode_execBadge()}
      </span>
    </div>

    <div class="px-3 sm:px-5 py-4 space-y-4">
      <p class="text-sm text-gray-600 dark:text-gray-400">{m.system_workerMode_description()}</p>

      <button
        type="button"
        onclick={() => pick('exec')}
        disabled={$workerModeLoading}
        aria-pressed={draft === 'exec'}
        class="w-full text-left flex items-start gap-3 p-3 rounded-sm border-2 transition-colors disabled:opacity-60 disabled:cursor-not-allowed {draft === 'exec'
          ? 'border-emerald-500 dark:border-emerald-400 bg-emerald-50 dark:bg-emerald-500/10 ring-1 ring-emerald-500/20'
          : 'border-gray-200 dark:border-lerd-border hover:border-gray-300 dark:hover:border-white/20 hover:bg-gray-50 dark:hover:bg-white/3'}"
      >
        <span
          class="mt-0.5 inline-flex items-center justify-center w-4 h-4 rounded-full border-2 shrink-0 {draft === 'exec'
            ? 'border-emerald-500 dark:border-emerald-400'
            : 'border-gray-300 dark:border-gray-500'}"
        >
          {#if draft === 'exec'}
            <span class="w-2 h-2 rounded-full bg-emerald-500 dark:bg-emerald-400"></span>
          {/if}
        </span>
        <span class="flex-1">
          <span class="block text-sm font-medium {draft === 'exec' ? 'text-emerald-900 dark:text-emerald-200' : 'text-gray-800 dark:text-gray-200'}">{m.system_workerMode_exec_title()}</span>
          <span class="block text-xs text-gray-500 dark:text-gray-400 mt-0.5">{m.system_workerMode_exec_description()}</span>
        </span>
      </button>

      <button
        type="button"
        onclick={() => pick('container')}
        disabled={$workerModeLoading}
        aria-pressed={draft === 'container'}
        class="w-full text-left flex items-start gap-3 p-3 rounded-sm border-2 transition-colors disabled:opacity-60 disabled:cursor-not-allowed {draft === 'container'
          ? 'border-sky-500 dark:border-sky-400 bg-sky-50 dark:bg-sky-500/10 ring-1 ring-sky-500/20'
          : 'border-gray-200 dark:border-lerd-border hover:border-gray-300 dark:hover:border-white/20 hover:bg-gray-50 dark:hover:bg-white/3'}"
      >
        <span
          class="mt-0.5 inline-flex items-center justify-center w-4 h-4 rounded-full border-2 shrink-0 {draft === 'container'
            ? 'border-sky-500 dark:border-sky-400'
            : 'border-gray-300 dark:border-gray-500'}"
        >
          {#if draft === 'container'}
            <span class="w-2 h-2 rounded-full bg-sky-500 dark:bg-sky-400"></span>
          {/if}
        </span>
        <span class="flex-1">
          <span class="block text-sm font-medium {draft === 'container' ? 'text-sky-900 dark:text-sky-200' : 'text-gray-800 dark:text-gray-200'}">{m.system_workerMode_container_title()}</span>
          <span class="block text-xs text-gray-500 dark:text-gray-400 mt-0.5">{m.system_workerMode_container_description()}</span>
        </span>
      </button>

      <div class="text-xs text-gray-500 dark:text-gray-400 bg-gray-50 dark:bg-lerd-card/50 rounded-sm px-3 py-2 border border-gray-200 dark:border-lerd-border">
        <span class="font-medium text-gray-700 dark:text-gray-300">{m.system_workerMode_note_label()}</span>
        {m.system_workerMode_note_body()}
      </div>

      <div class="flex items-center justify-end gap-2 pt-2 border-t border-gray-100 dark:border-lerd-border">
        {#if dirty}
          <button
            type="button"
            onclick={() => (draft = $workerExecMode)}
            disabled={$workerModeLoading}
            class="px-3 py-1.5 rounded-lg text-sm font-medium bg-gray-100 hover:bg-gray-200 dark:bg-white/5 dark:hover:bg-white/10 text-gray-700 dark:text-gray-300 disabled:opacity-50 transition-colors"
          >{m.common_cancel()}</button>
        {/if}
        <button
          type="button"
          onclick={openConfirm}
          disabled={!dirty || $workerModeLoading}
          class="px-3 py-1.5 rounded-lg text-sm font-medium bg-lerd-red hover:bg-lerd-redhov text-white disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
        >{m.common_save()}</button>
      </div>
    </div>
  </div>

  {#snippet confirmFooter()}
    <button
      type="button"
      onclick={cancelConfirm}
      disabled={$workerModeLoading}
      class="px-3 py-1.5 rounded-lg text-sm font-medium bg-gray-100 hover:bg-gray-200 dark:bg-white/5 dark:hover:bg-white/10 text-gray-700 dark:text-gray-300 disabled:opacity-50 transition-colors"
    >{m.common_cancel()}</button>
    <button
      type="button"
      onclick={applyChange}
      disabled={$workerModeLoading}
      class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium bg-lerd-red hover:bg-lerd-redhov text-white disabled:opacity-60 transition-colors"
    >
      {#if $workerModeLoading}
        <svg class="w-3.5 h-3.5 animate-spin" fill="none" viewBox="0 0 24 24">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"/>
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"/>
        </svg>
        <span class="truncate max-w-[26ch]">{progressLabel($workerModeProgress)}</span>
      {:else}
        {m.system_workerMode_apply_confirm()}
      {/if}
    </button>
  {/snippet}

  <Modal open={confirmOpen} title={m.system_workerMode_confirm_title()} onclose={cancelConfirm} size="sm" footer={confirmFooter}>
    <div class="px-5 py-4 space-y-3">
      <p class="text-sm text-gray-700 dark:text-gray-300">
        {m.system_workerMode_confirm_body({
          from: $workerExecMode === 'container' ? m.system_workerMode_containerBadge() : m.system_workerMode_execBadge(),
          to: draft === 'container' ? m.system_workerMode_containerBadge() : m.system_workerMode_execBadge()
        })}
      </p>
      <p class="text-xs text-gray-500 dark:text-gray-400">{m.system_workerMode_confirm_hint()}</p>
      {#if applyError}
        <p class="text-xs text-red-500">{applyError}</p>
      {/if}
    </div>
  </Modal>
{/if}
