<script lang="ts">
  import { goToTab } from '$stores/route';
  import { services, serviceLabel } from '$stores/services';
  import type { Site } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  function jump(name: string) {
    goToTab('services', name);
  }

  function svcActive(name: string): boolean {
    return $services.find((s) => s.name === name)?.status === 'active';
  }

  const showFrankenphp = $derived(!site.custom_container && site.runtime === 'frankenphp');

  const hasAny = $derived((site.services && site.services.length > 0) || showFrankenphp);
</script>

{#if hasAny}
  <div class="flex items-center flex-wrap gap-3">
    {#each site.services || [] as name (name)}
      <button
        onclick={() => jump(name)}
        title={'Open ' + name + ' service'}
        class="inline-flex items-center gap-1.5 text-[11px] font-medium px-2 py-0.5 rounded-full border transition-colors {svcActive(name)
          ? 'bg-emerald-50 dark:bg-emerald-500/10 border-emerald-200 dark:border-emerald-500/30 text-emerald-700 dark:text-emerald-400 hover:bg-emerald-100 dark:hover:bg-emerald-500/20'
          : 'bg-gray-50 dark:bg-white/5 border-gray-200 dark:border-lerd-border text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-white/10'}"
      >
        <span class="w-1.5 h-1.5 rounded-full {svcActive(name) ? 'bg-emerald-500' : 'bg-gray-400 dark:bg-gray-600'}"></span>
        {serviceLabel(name)}
      </button>
    {/each}

    {#if showFrankenphp}
      <span
        title={site.runtime_worker ? m.sites_badges_frankenphpWorkerTitle() : m.sites_badges_frankenphp()}
        class="inline-flex items-center gap-1.5 text-[11px] font-medium px-2 py-0.5 rounded-full border bg-orange-50 dark:bg-orange-500/10 border-orange-200 dark:border-orange-500/30 text-orange-700 dark:text-orange-300"
      >
        <span class="w-1.5 h-1.5 rounded-full bg-orange-500"></span>
        <span>{m.sites_badges_frankenphp()}</span>
        {#if site.runtime_worker}
          <span class="opacity-70">{m.sites_badges_frankenphpWorker()}</span>
        {/if}
      </span>
    {/if}
  </div>
{/if}
