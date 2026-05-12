<script lang="ts">
  import {
    unhealthyWorkers,
    healLoading,
    healProgressUnit,
    healDoneCount,
    healTotalCount,
    healAll,
    loadWorkerHealth
  } from '$stores/workerHealth';
  import { m } from '../paraglide/messages.js';

  // In-session dismiss only: deliberately not persisted to localStorage so
  // a fresh tab always re-surfaces failing workers. The set tracks the
  // canonical signature of the unhealthy list at dismiss time, so when a
  // NEW worker fails after dismissal the banner reappears.
  let dismissedSignature = $state<string | null>(null);
  const currentSignature = $derived(
    $unhealthyWorkers
      .map((u) => u.unit)
      .slice()
      .sort()
      .join(',')
  );
  // Stay visible while healing even if the underlying list briefly reaches
  // zero — otherwise the loader would unmount mid-stream and the user
  // wouldn't see the final "Healing 3 of 3" tick.
  const visible = $derived(
    ($healLoading || $unhealthyWorkers.length > 0) && currentSignature !== dismissedSignature
  );

  // progressPercent is a deliberate 0-100 derivation so the bar advances
  // smoothly as units complete. When total is unknown (zero) we keep the
  // bar at indeterminate width via CSS.
  const progressPercent = $derived(
    $healTotalCount > 0 ? Math.min(100, Math.round(($healDoneCount / $healTotalCount) * 100)) : 0
  );

  async function onHeal() {
    const r = await healAll();
    await loadWorkerHealth();
    if (!r.ok && r.error) {
      console.error('[lerd] heal failed:', r.error);
    }
  }

  function onDismiss() {
    dismissedSignature = currentSignature;
  }
</script>

{#if visible}
  <div class="fixed bottom-3 left-1/2 -translate-x-1/2 z-60 w-[min(92vw,640px)] rounded-lg border-l-4 border border-amber-400 dark:border-amber-500/50 border-l-amber-500 bg-white/85 dark:bg-lerd-card/80 backdrop-blur-md shadow-2xl overflow-hidden">
    {#if $healLoading}
      <div class="h-1 bg-amber-200/40 dark:bg-amber-500/20">
        <div
          class="h-full bg-amber-500 transition-[width] duration-300 ease-out"
          style="width: {progressPercent}%"
        ></div>
      </div>
    {/if}
    <div class="flex items-center gap-3 px-3 py-2.5">
      {#if $healLoading}
        <svg class="w-5 h-5 shrink-0 text-amber-600 dark:text-amber-400 animate-spin" fill="none" viewBox="0 0 24 24" aria-hidden="true">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"/>
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"/>
        </svg>
      {:else}
        <svg class="w-5 h-5 shrink-0 text-amber-600 dark:text-amber-400" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24" aria-hidden="true">
          <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"/>
        </svg>
      {/if}
      <div class="flex-1 min-w-0">
        {#if $healLoading}
          <p class="text-xs font-semibold text-amber-900 dark:text-amber-200">
            {m.workers_health_banner_progress({
              done: $healDoneCount,
              total: $healTotalCount
            })}
          </p>
          <p class="text-[11px] text-amber-700 dark:text-amber-300/80 mt-0.5 truncate">
            {$healProgressUnit ?? ''}
          </p>
        {:else}
          <p class="text-xs font-semibold text-amber-900 dark:text-amber-200">
            {m.workers_health_banner_title({ count: $unhealthyWorkers.length })}
          </p>
          <p class="text-[11px] text-amber-700 dark:text-amber-300/80 mt-0.5 truncate">
            {$unhealthyWorkers.map((u) => `${u.worker}@${u.site}`).join(', ')}
          </p>
        {/if}
      </div>
      <button
        onclick={onHeal}
        disabled={$healLoading}
        class="shrink-0 inline-flex items-center gap-1.5 text-xs font-medium bg-amber-600 hover:bg-amber-700 text-white rounded-sm px-3 py-1.5 transition-colors disabled:opacity-50"
      >
        {#if $healLoading}
          {m.workers_health_banner_healing()}
        {:else}
          {m.workers_health_banner_heal()}
        {/if}
      </button>
      <button
        onclick={onDismiss}
        disabled={$healLoading}
        title={m.workers_health_banner_dismiss()}
        aria-label={m.workers_health_banner_dismiss()}
        class="shrink-0 text-amber-600/60 hover:text-amber-700 dark:text-amber-400/60 dark:hover:text-amber-300 transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
      >
        <svg class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12"/>
        </svg>
      </button>
    </div>
  </div>
{/if}
