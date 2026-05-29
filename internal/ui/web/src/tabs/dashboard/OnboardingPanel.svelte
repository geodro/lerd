<script lang="ts">
  import { onMount } from 'svelte';
  import { sites, sitesLoaded } from '$stores/sites';
  import { openLinkModal, openPresetModal } from '$stores/modals';
  import { openDocs } from '$stores/dashboard';
  import { accessMode } from '$stores/accessMode';
  import { m } from '../../paraglide/messages.js';

  const KEY = 'lerd-onboarding-dismissed';

  let dismissed = $state(false);

  onMount(() => {
    try {
      dismissed = localStorage.getItem(KEY) === '1';
    } catch {
      dismissed = false;
    }
  });

  function dismiss() {
    dismissed = true;
    try {
      localStorage.setItem(KEY, '1');
    } catch {
      /* incognito or storage disabled */
    }
  }

  const visible = $derived($sitesLoaded && $sites.length === 0 && !dismissed);
</script>

{#if visible}
  <div class="relative rounded-xl bg-linear-to-br from-lerd-red/10 via-lerd-red/5 to-transparent dark:from-lerd-red/15 dark:via-lerd-red/5 border border-lerd-red/20 dark:border-lerd-red/30 px-5 py-5 sm:py-6">
    <button
      type="button"
      onclick={dismiss}
      title={m.onboarding_dismiss()}
      aria-label={m.onboarding_dismiss()}
      class="absolute top-3 right-3 w-7 h-7 inline-flex items-center justify-center rounded-sm text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 hover:bg-white/40 dark:hover:bg-white/10 transition-colors"
    >
      <svg class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24" aria-hidden="true">
        <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12"/>
      </svg>
    </button>

    <div class="max-w-2xl">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white tracking-tight">{m.onboarding_title()}</h2>
      <p class="text-sm text-gray-600 dark:text-gray-300 mt-1">{m.onboarding_subtitle()}</p>
    </div>

    <div class="mt-5 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
      <div class="rounded-lg bg-white/70 dark:bg-lerd-card/60 border border-gray-100 dark:border-lerd-border px-3 py-3">
        <div class="flex items-center gap-2 mb-2">
          <span class="w-5 h-5 inline-flex items-center justify-center rounded-full bg-lerd-red/15 text-lerd-red text-[11px] font-semibold">1</span>
          <span class="text-sm font-semibold text-gray-800 dark:text-gray-100">{m.onboarding_park_title()}</span>
        </div>
        <p class="text-xs text-gray-500 dark:text-gray-400 mb-2">{m.onboarding_park_body()}</p>
        <code class="block text-xs font-mono bg-gray-100 dark:bg-white/5 text-gray-700 dark:text-gray-300 rounded-sm px-2 py-1.5 overflow-x-auto">lerd park ~/Code</code>
      </div>

      <div class="rounded-lg bg-white/70 dark:bg-lerd-card/60 border border-gray-100 dark:border-lerd-border px-3 py-3 flex flex-col">
        <div class="flex items-center gap-2 mb-2">
          <span class="w-5 h-5 inline-flex items-center justify-center rounded-full bg-lerd-red/15 text-lerd-red text-[11px] font-semibold">2</span>
          <span class="text-sm font-semibold text-gray-800 dark:text-gray-100">{m.onboarding_link_title()}</span>
        </div>
        <p class="text-xs text-gray-500 dark:text-gray-400 mb-3 flex-1">{m.onboarding_link_body()}</p>
        {#if $accessMode.loopback}
          <button
            type="button"
            onclick={openLinkModal}
            class="self-start inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium bg-lerd-red hover:bg-lerd-redhov text-white transition-colors"
          >
            <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4"/>
            </svg>
            {m.onboarding_link_cta()}
          </button>
        {:else}
          <p class="text-[11px] text-gray-400 dark:text-gray-500">{m.onboarding_loopbackOnly()}</p>
        {/if}
      </div>

      <div class="rounded-lg bg-white/70 dark:bg-lerd-card/60 border border-gray-100 dark:border-lerd-border px-3 py-3 flex flex-col">
        <div class="flex items-center gap-2 mb-2">
          <span class="w-5 h-5 inline-flex items-center justify-center rounded-full bg-lerd-red/15 text-lerd-red text-[11px] font-semibold">3</span>
          <span class="text-sm font-semibold text-gray-800 dark:text-gray-100">{m.onboarding_service_title()}</span>
        </div>
        <p class="text-xs text-gray-500 dark:text-gray-400 mb-3 flex-1">{m.onboarding_service_body()}</p>
        {#if $accessMode.loopback}
          <button
            type="button"
            onclick={openPresetModal}
            class="self-start inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium bg-gray-100 hover:bg-gray-200 dark:bg-white/10 dark:hover:bg-white/20 text-gray-700 dark:text-gray-200 transition-colors"
          >{m.onboarding_service_cta()}</button>
        {:else}
          <p class="text-[11px] text-gray-400 dark:text-gray-500">{m.onboarding_loopbackOnly()}</p>
        {/if}
      </div>
    </div>

    <div class="mt-4 flex items-center gap-3 text-xs text-gray-500 dark:text-gray-400">
      <button
        type="button"
        onclick={openDocs}
        class="font-medium text-lerd-red hover:text-lerd-redhov transition-colors"
      >{m.onboarding_docs()}</button>
      <span class="text-gray-300 dark:text-gray-600">·</span>
      <span>{m.onboarding_dismissHint()}</span>
    </div>
  </div>
{/if}
