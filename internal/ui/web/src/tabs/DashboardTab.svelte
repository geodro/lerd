<script lang="ts">
  import { version } from '$stores/version';
  import { sites } from '$stores/sites';
  import { coreServices } from '$stores/services';
  import { unhealthyWorkers } from '$stores/workerHealth';
  import { status, statusLoaded } from '$stores/status';
  import HeroStatus from './dashboard/HeroStatus.svelte';
  import OnboardingPanel from './dashboard/OnboardingPanel.svelte';
  import SystemHealthWidget from './dashboard/SystemHealthWidget.svelte';
  import LerdInfoWidget from './dashboard/LerdInfoWidget.svelte';
  import SitesWidget from './dashboard/SitesWidget.svelte';
  import ServicesWidget from './dashboard/ServicesWidget.svelte';
  import WorkersWidget from './dashboard/WorkersWidget.svelte';
  import ResourcesWidget from './dashboard/ResourcesWidget.svelte';
  import { openCommandPalette } from '$stores/commandPalette';
  import { m } from '../paraglide/messages.js';

  // Inline summary used as the dashboard subtitle when everything is
  // healthy. The full hero strip only renders for problems / updates so
  // healthy state doesn't burn a row of vertical space.
  const sitesRunning = $derived($sites.filter((s) => s.fpm_running && !s.paused).length);
  const sitesTotal = $derived($sites.length);
  const servicesActive = $derived($coreServices.filter((s) => s.status === 'active').length);
  const everythingHealthy = $derived(
    $unhealthyWorkers.length === 0 &&
      $statusLoaded &&
      $status.dns.ok &&
      $status.nginx.running &&
      $status.watcher_running &&
      !$version.hasUpdate &&
      !$coreServices.some((s) => s.update_available)
  );
</script>

<div class="flex-1 overflow-y-auto">
  <div class="flex flex-wrap items-center justify-between gap-y-2 px-3 py-3 border-b border-gray-100 dark:border-lerd-border">
    <div class="min-w-0">
      <h1 class="font-semibold text-gray-900 dark:text-white text-xl tracking-tight">{m.dashboard_title()}</h1>
      {#if everythingHealthy}
        <p class="text-xs text-emerald-700 dark:text-emerald-400 mt-0.5 inline-flex items-center gap-1.5">
          <span class="relative inline-flex w-1.5 h-1.5">
            <span class="absolute inline-flex w-full h-full rounded-full bg-emerald-400 opacity-70 animate-ping"></span>
            <span class="relative inline-flex w-1.5 h-1.5 rounded-full bg-emerald-500"></span>
          </span>
          {m.dashboard_hero_allGood({ sitesRunning, sitesTotal, servicesActive })}
        </p>
      {:else}
        <p class="text-xs text-gray-500 dark:text-gray-400 mt-0.5">{m.dashboard_subtitle()}</p>
      {/if}
    </div>
    <button
      type="button"
      onclick={openCommandPalette}
      title={m.dashboard_searchHint()}
      class="hidden sm:inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full bg-gray-100 hover:bg-gray-200 dark:bg-white/5 dark:hover:bg-white/10 text-gray-500 dark:text-gray-400 transition-colors"
    >
      <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"/>
      </svg>
      <span class="text-xs">{m.dashboard_search()}</span>
      <kbd class="text-[10px] font-mono bg-white dark:bg-white/10 border border-gray-200 dark:border-lerd-border rounded-sm px-1 py-px">⌘K</kbd>
    </button>
  </div>

  <div class="p-3 space-y-3">
    <OnboardingPanel />
    {#if !everythingHealthy}
      <HeroStatus />
    {/if}
    <div class="grid grid-cols-1 lg:grid-cols-2 xl:grid-cols-3 gap-3">
      <SitesWidget />
      <ServicesWidget />
      <WorkersWidget />
      <SystemHealthWidget />
      <ResourcesWidget />
      <LerdInfoWidget />
    </div>
  </div>
</div>
