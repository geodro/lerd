<script lang="ts">
  import {
    permissionState,
    dismissed,
    enableNotifications,
    dismissNotifyBanner
  } from '$lib/notify';
  import { m } from '../paraglide/messages.js';

  const visible = $derived($permissionState === 'default' && !$dismissed);

  async function onEnable() {
    await enableNotifications();
  }
  function onDismiss() {
    dismissNotifyBanner();
  }
</script>

{#if visible}
  <div class="fixed bottom-3 left-1/2 -translate-x-1/2 z-60 w-[min(92vw,640px)] rounded-lg border-l-4 border border-sky-300 dark:border-sky-500/40 border-l-sky-500 bg-white/85 dark:bg-lerd-card/80 backdrop-blur-md shadow-2xl">
    <div class="flex items-center gap-3 px-3 py-2.5">
      <svg class="w-5 h-5 shrink-0 text-sky-600 dark:text-sky-400" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24" aria-hidden="true">
        <path stroke-linecap="round" stroke-linejoin="round" d="M15 17h5l-1.4-1.4A2 2 0 0118 14.2V11a6 6 0 10-12 0v3.2a2 2 0 01-.6 1.4L4 17h5m6 0a3 3 0 11-6 0"/>
      </svg>
      <div class="flex-1 min-w-0">
        <p class="text-xs font-semibold text-sky-900 dark:text-sky-200">
          {m.notify_banner_title()}
        </p>
        <p class="text-[11px] text-sky-700 dark:text-sky-300/80 mt-0.5">
          {m.notify_banner_subtitle()}
        </p>
      </div>
      <button
        onclick={onEnable}
        class="shrink-0 inline-flex items-center text-xs font-medium bg-sky-600 hover:bg-sky-700 text-white rounded-sm px-3 py-1.5 transition-colors"
      >{m.notify_banner_enable()}</button>
      <button
        onclick={onDismiss}
        title={m.notify_banner_dismiss()}
        aria-label={m.notify_banner_dismiss()}
        class="shrink-0 text-sky-600/60 hover:text-sky-700 dark:text-sky-400/60 dark:hover:text-sky-300 transition-colors"
      >
        <svg class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12"/>
        </svg>
      </button>
    </div>
  </div>
{/if}
