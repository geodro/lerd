<script lang="ts">
  import SiteIcon from '$components/SiteIcon.svelte';
  import SiteIndicators from '$components/SiteIndicators.svelte';
  import Icon from '$components/Icon.svelte';
  import { goToTab } from '$stores/route';
  import { openSiteInBrowser, type Site } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  const subtitle = $derived.by(() => {
    const parts: string[] = [];
    if (site.framework_label) parts.push(site.framework_label);
    if (site.php_version) parts.push('PHP ' + site.php_version);
    else if (site.node_version) parts.push('Node ' + site.node_version);
    return parts.join(' · ');
  });

  function openBrowser(e: MouseEvent) {
    e.stopPropagation();
    openSiteInBrowser(site);
  }
</script>

<div
  class="group flex items-center gap-3 rounded-xl border border-gray-200/80 dark:border-lerd-border bg-white dark:bg-lerd-card p-3 transition duration-150 hover:-translate-y-0.5 hover:shadow-lg hover:shadow-black/5 hover:border-gray-300 dark:hover:border-white/15 {site.paused ? 'opacity-60' : ''}"
>
  <button onclick={() => goToTab('sites', site.domain)} class="flex items-center gap-3 min-w-0 flex-1 text-left">
    <span class="shrink-0 inline-flex items-center justify-center w-9 h-9 rounded-lg bg-gray-100 dark:bg-white/5 transition-transform group-hover:scale-105">
      <SiteIcon {site} size="w-5 h-5" />
    </span>
    <div class="min-w-0 flex-1">
      <div class="flex items-center gap-1.5">
        <span class="text-sm font-semibold text-gray-900 dark:text-white truncate" title={site.domain}>{site.domain}</span>
        {#if site.tls}
          <svg class="w-3 h-3 shrink-0 text-emerald-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z"/>
          </svg>
        {/if}
      </div>
      {#if subtitle}
        <p class="text-[11px] leading-snug text-gray-500 dark:text-gray-400 truncate" title={subtitle}>{subtitle}</p>
      {/if}
    </div>
  </button>

  <div class="shrink-0 flex items-center gap-1.5">
    <SiteIndicators {site} />
    {#if !site.paused}
      <button
        onclick={openBrowser}
        title={m.sites_openInBrowser()}
        aria-label={m.sites_openInBrowser()}
        class="ml-0.5 inline-flex items-center justify-center w-7 h-7 rounded-lg text-gray-400 dark:text-gray-500 hover:bg-gray-100 dark:hover:bg-white/5 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
      >
        <Icon name="external" class="w-3.5 h-3.5" />
      </button>
    {/if}
  </div>
</div>
