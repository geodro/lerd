<script lang="ts">
  import { dashboardOpen, closeDashboard } from '$stores/dashboard';
  import { dashboardIconSvg } from '$lib/dashboardIcons';
</script>

{#if $dashboardOpen}
  {@const d = $dashboardOpen}
  <div class="fixed top-0 right-0 left-0 bottom-16 md:left-14 md:bottom-0 z-30 flex flex-col bg-white dark:bg-lerd-bg">
    <div class="flex items-center justify-between px-4 py-2 border-b border-gray-200 dark:border-lerd-border shrink-0">
      <div class="flex items-center gap-3 min-w-0">
        <svg class="w-5 h-5 text-gray-500 dark:text-gray-400 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          {@html dashboardIconSvg(d.name)}
        </svg>
        <span class="text-sm font-medium text-gray-900 dark:text-white truncate">{d.label || d.name}</span>
        <a
          href={d.dashboard}
          target="_blank"
          rel="noopener"
          class="font-mono text-[10px] text-sky-600 dark:text-sky-400 hover:underline truncate"
        >{d.dashboard}</a>
      </div>
      <div class="flex items-center gap-2 shrink-0">
        <a
          href={d.dashboard}
          target="_blank"
          rel="noopener"
          class="text-xs text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 border border-gray-200 dark:border-lerd-border hover:border-gray-300 dark:hover:border-lerd-muted rounded px-2 py-1 transition-colors"
        >Open in new tab</a>
        <button
          onclick={closeDashboard}
          title="Close"
          aria-label="Close dashboard"
          class="text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
        >
          <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/>
          </svg>
        </button>
      </div>
    </div>
    <iframe src={d.dashboard} class="flex-1 w-full bg-white border-0" title={d.label || d.name}></iframe>
  </div>
{/if}
