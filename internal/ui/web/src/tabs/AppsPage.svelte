<script lang="ts">
  import { dashboardServices, openDashboard } from '$stores/dashboard';
  import { dashboardIconSvg } from '$lib/dashboardIcons';
  import { serviceLabel } from '$stores/services';
  import { m } from '../paraglide/messages.js';
</script>

<div class="flex-1 overflow-y-auto p-4">
  {#if $dashboardServices.length === 0}
    <div class="text-center text-sm text-gray-400 dark:text-gray-500 py-16">
      {m.apps_empty()}
    </div>
  {:else}
    <div class="grid grid-cols-3 gap-3">
      {#each $dashboardServices as svc (svc.name)}
        <button
          onclick={() => openDashboard(svc)}
          class="flex flex-col items-center justify-center gap-2 aspect-square rounded-xl border border-gray-200 dark:border-lerd-border bg-white dark:bg-lerd-card hover:border-lerd-red/40 hover:bg-lerd-red/5 transition-colors p-3"
        >
          <svg class="w-7 h-7 text-gray-500 dark:text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            {@html dashboardIconSvg(svc.name)}
          </svg>
          <span class="text-[11px] font-medium text-gray-700 dark:text-gray-300 text-center truncate max-w-full">
            {serviceLabel(svc.name)}
          </span>
        </button>
      {/each}
    </div>
  {/if}
</div>
