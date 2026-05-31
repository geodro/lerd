<script lang="ts">
  import {
    unhealthyWorkers,
    healAll,
    healLoading,
    healDoneCount,
    healTotalCount,
    loadWorkerHealth
  } from '$stores/workerHealth';
  import { version } from '$stores/version';
  import { coreServices } from '$stores/services';
  import { sites } from '$stores/sites';
  import { status, statusLoaded, dnsState } from '$stores/status';
  import { accessMode } from '$stores/accessMode';
  import { goToTab } from '$stores/route';
  import { apiFetch } from '$lib/api';
  import { m } from '../../paraglide/messages.js';

  type HeroPriority = 'error' | 'updates' | 'ok';

  const failingWorkers = $derived($unhealthyWorkers.length);
  const hasLerdUpdate = $derived($version.hasUpdate);

  const coreDown = $derived.by(() => {
    if (!$statusLoaded) return [] as string[];
    const issues: string[] = [];
    // Only a genuine lerd-dns outage is a core failure. "degraded" means
    // lerd-dns is healthy but the system resolver is bypassed (typically a
    // VPN), which lerd recovers from on its own, so it doesn't belong here.
    if ($status.dns?.enabled !== false && dnsState($status) === 'down') issues.push('DNS');
    if (!$status.nginx.running) issues.push('Nginx');
    if (!$status.watcher_running) issues.push('Watcher');
    return issues;
  });

  const priority = $derived.by((): HeroPriority => {
    if (failingWorkers > 0 || coreDown.length > 0) return 'error';
    if (hasLerdUpdate) return 'updates';
    return 'ok';
  });

  const sitesRunning = $derived($sites.filter((s) => s.fpm_running && !s.paused).length);
  const sitesTotal = $derived($sites.length);
  const servicesActive = $derived($coreServices.filter((s) => s.status === 'active').length);

  let updateTerminalLoading = $state(false);

  async function onHeal() {
    await healAll();
    await loadWorkerHealth();
  }

  async function onUpdateLerd() {
    updateTerminalLoading = true;
    try {
      await apiFetch('/api/lerd/update-terminal', { method: 'POST' });
    } finally {
      updateTerminalLoading = false;
    }
  }

  const failingWorkerSites = $derived.by(() => {
    const set = new Set<string>();
    for (const u of $unhealthyWorkers) set.add(u.site);
    return [...set];
  });
</script>

{#if priority === 'error'}
  <div class="rounded-xl border-l-4 border-l-red-500 border border-red-200 dark:border-red-500/30 bg-red-50 dark:bg-red-500/10 px-3 py-3">
    <div class="flex flex-wrap items-center gap-3">
      <span class="relative flex shrink-0">
        <span class="animate-ping absolute inline-flex h-2.5 w-2.5 rounded-full bg-red-400 opacity-75"></span>
        <span class="relative inline-flex h-2.5 w-2.5 rounded-full bg-red-500"></span>
      </span>
      <div class="flex-1 min-w-0">
        {#if coreDown.length > 0}
          <p class="text-sm font-semibold text-red-900 dark:text-red-200">
            {m.dashboard_hero_coreDown({ components: coreDown.join(', ') })}
          </p>
          <p class="text-xs text-red-700 dark:text-red-300/80 mt-0.5 truncate">
            {m.dashboard_hero_coreDownHint()}
          </p>
        {:else}
          <p class="text-sm font-semibold text-red-900 dark:text-red-200">
            {m.dashboard_hero_workersFailing({ count: failingWorkers })}
          </p>
          <p class="text-xs text-red-700 dark:text-red-300/80 mt-0.5 truncate">
            {failingWorkerSites.join(', ')}
          </p>
        {/if}
      </div>
      {#if coreDown.length > 0}
        <button
          onclick={() => goToTab('system', 'lerd')}
          class="shrink-0 inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-red-600 hover:bg-red-700 text-white transition-colors"
        >{m.dashboard_hero_openSystem()}</button>
      {:else}
        <button
          onclick={onHeal}
          disabled={$healLoading}
          class="shrink-0 inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-amber-600 hover:bg-amber-700 text-white disabled:opacity-50 transition-colors"
        >
          {#if $healLoading}
            {m.dashboard_workers_healing({ done: $healDoneCount, total: $healTotalCount, pct: $healTotalCount > 0 ? Math.round(($healDoneCount / $healTotalCount) * 100) : 0 })}
          {:else}
            {m.dashboard_workers_healAll()}
          {/if}
        </button>
      {/if}
    </div>
  </div>
{:else if priority === 'updates'}
  <div class="rounded-xl border-l-4 border-l-yellow-500 border border-yellow-200 dark:border-yellow-500/30 bg-yellow-50 dark:bg-yellow-500/10 px-3 py-3">
    <div class="flex flex-wrap items-center gap-3">
      <svg class="w-5 h-5 shrink-0 text-yellow-600 dark:text-yellow-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 16V4m0 0L3 8m4-4l4 4m6 0v12m0 0l4-4m-4 4l-4-4"/>
      </svg>
      <div class="flex-1 min-w-0">
        <p class="text-sm font-semibold text-yellow-900 dark:text-yellow-200">
          {m.dashboard_hero_lerdUpdate({ version: $version.latest })}
        </p>
      </div>
      {#if $accessMode.loopback}
        <button
          onclick={onUpdateLerd}
          disabled={updateTerminalLoading}
          class="shrink-0 inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-yellow-600 hover:bg-yellow-700 text-white disabled:opacity-50 transition-colors"
        >
          {updateTerminalLoading ? m.system_lerd_openingTerminal() : m.system_lerd_openTerminal()}
        </button>
      {/if}
    </div>
  </div>
{:else}
  <div class="rounded-xl border border-emerald-200/70 dark:border-emerald-500/20 bg-emerald-50/60 dark:bg-emerald-500/5 px-3 py-2 flex items-center gap-3">
    <span class="relative flex shrink-0">
      <span class="absolute inline-flex h-2 w-2 rounded-full bg-emerald-400 opacity-75 animate-ping"></span>
      <span class="relative inline-flex h-2 w-2 rounded-full bg-emerald-500"></span>
    </span>
    <p class="text-xs font-medium text-emerald-800 dark:text-emerald-300">
      {m.dashboard_hero_allGood({
        sitesRunning,
        sitesTotal,
        servicesActive
      })}
    </p>
  </div>
{/if}
