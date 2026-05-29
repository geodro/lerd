<script lang="ts">
  import ParentSiteBadge from './ParentSiteBadge.svelte';
  import { goToTab } from '$stores/route';
  import { type Service, isServiceWorker, parentSiteDomain } from '$stores/services';

  interface Props {
    svc: Service;
  }
  let { svc }: Props = $props();

  const isWorker = $derived(isServiceWorker(svc));
  const parent = $derived(parentSiteDomain(svc));
  const siteDomains = $derived(!isWorker && svc.site_domains ? svc.site_domains : []);
  const hasBadges = $derived(Boolean(parent) || siteDomains.length > 0);
</script>

{#if hasBadges}
  <div class="flex flex-wrap items-center gap-1 px-3 pt-3 shrink-0">
    {#if parent}
      <ParentSiteBadge domain={parent} />
    {/if}
    {#each siteDomains as d (d)}
      <button
        onclick={() => goToTab('sites', d)}
        class="inline-flex items-center gap-1.5 text-xs font-medium bg-gray-100 dark:bg-white/5 hover:bg-gray-200 dark:hover:bg-white/10 border border-gray-200 dark:border-lerd-border text-gray-700 dark:text-gray-300 rounded-full px-2 py-0.5 transition-colors"
      >
        <span class="w-1.5 h-1.5 rounded-full shrink-0 bg-gray-400"></span>
        {d}
      </button>
    {/each}
  </div>
{/if}
