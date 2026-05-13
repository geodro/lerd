<script lang="ts">
  import DashboardCard from './DashboardCard.svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import StatusDot from '$components/StatusDot.svelte';
  import { workerGroups, workerSiteName, parentSiteDomain, type Service } from '$stores/services';
  import { sites } from '$stores/sites';
  import { get } from 'svelte/store';
  import {
    unhealthyWorkers,
    healAll,
    healLoading,
    healDoneCount,
    healTotalCount,
    loadWorkerHealth
  } from '$stores/workerHealth';
  import { goToTab } from '$stores/route';
  import { m } from '../../paraglide/messages.js';

  const groups = $derived($workerGroups);
  const totalUnits = $derived(groups.reduce((n, g) => n + g.items.length, 0));
  const totalActive = $derived(
    groups.reduce((n, g) => n + g.items.filter((i) => i.status === 'active').length, 0)
  );
  const failingCount = $derived($unhealthyWorkers.length);

  function isItemFailing(item: Service): boolean {
    return $unhealthyWorkers.some((u) => u.unit === item.name || u.unit === 'lerd-' + item.name);
  }

  function jumpToSite(item: Service) {
    const domain = parentSiteDomain(item);
    if (domain) {
      goToTab('sites', domain);
      return;
    }
    // Last-chance lookup: scan the sites store directly. Hard-coding
    // a TLD here breaks for custom-domain sites and for users running
    // on a non-default .test TLD.
    const name = workerSiteName(item).split('/')[0];
    const site = get(sites).find((x) => x.name === name);
    if (site && site.domain) {
      goToTab('sites', site.domain);
    }
  }

  async function onHeal() {
    const r = await healAll();
    await loadWorkerHealth();
    if (!r.ok && r.error) console.error('[lerd] heal failed:', r.error);
  }
</script>

<DashboardCard title={m.dashboard_workers_title()} tone={failingCount > 0 ? 'critical' : 'default'}>
  {#snippet badge()}
    {#if failingCount > 0}
      <StatusPill tone="error" label={m.dashboard_workers_failing({ count: failingCount })} />
    {:else if totalUnits > 0}
      <StatusPill tone="ok" label={m.dashboard_workers_summary({ active: totalActive, total: totalUnits })} />
    {:else}
      <StatusPill tone="muted" label={m.dashboard_workers_none()} />
    {/if}
  {/snippet}

  {#if totalUnits === 0}
    <p class="text-sm text-gray-500 dark:text-gray-400">{m.dashboard_workers_empty()}</p>
  {:else}
    {#if failingCount > 0}
      <div class="space-y-1.5">
        {#each $unhealthyWorkers as u (u.unit)}
          <div class="flex items-start gap-2 rounded-md border border-red-100 dark:border-red-500/20 bg-red-50/60 dark:bg-red-500/5 px-2.5 py-1.5">
            <StatusDot color="red" size="xs" pulse />
            <div class="flex-1 min-w-0">
              <p class="text-xs font-medium text-red-800 dark:text-red-300 truncate">{u.worker}@{u.site}</p>
              {#if u.last_error}
                <p title={u.last_error} class="text-[11px] font-mono text-red-700/80 dark:text-red-300/70 truncate">{u.last_error}</p>
              {/if}
            </div>
          </div>
        {/each}
      </div>
      <div class="border-t border-gray-100 dark:border-lerd-border"></div>
    {/if}
    <div class="space-y-2">
      {#each groups as g (g.key)}
        {@const active = g.items.filter((i) => i.status === 'active').length}
        <div>
          <div class="flex items-center justify-between text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
            <span>{g.label}</span>
            <span class="font-mono tabular-nums {active === g.items.length ? 'text-emerald-600 dark:text-emerald-500' : 'text-gray-500 dark:text-gray-400'}">{active}/{g.items.length}</span>
          </div>
          <div class="mt-1 space-y-0.5">
            {#each g.items as item (item.name)}
              {@const failing = isItemFailing(item)}
              <button
                type="button"
                onclick={() => jumpToSite(item)}
                class="group w-full flex items-center gap-2 px-1 py-0.5 -mx-1 rounded-sm text-left text-xs hover:bg-gray-50 dark:hover:bg-white/3 transition-colors"
              >
                <StatusDot
                  color={failing ? 'red' : item.status === 'active' ? 'green' : 'gray'}
                  size="xs"
                  pulse={failing}
                />
                <span class="flex-1 truncate text-gray-600 dark:text-gray-300 group-hover:text-lerd-red transition-colors">{workerSiteName(item)}</span>
              </button>
            {/each}
          </div>
        </div>
      {/each}
    </div>
  {/if}

  {#snippet footer()}
    {#if failingCount > 0}
      {@const pct = $healTotalCount > 0 ? Math.round(($healDoneCount / $healTotalCount) * 100) : 0}
      <button
        onclick={onHeal}
        disabled={$healLoading}
        class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium bg-amber-600 hover:bg-amber-700 text-white disabled:opacity-50 transition-colors"
      >
        {#if $healLoading}
          {m.dashboard_workers_healing({ done: $healDoneCount, total: $healTotalCount, pct })}
        {:else}
          {m.dashboard_workers_healAll()}
        {/if}
      </button>
    {:else}
      <span class="text-xs text-gray-400 dark:text-gray-500">{m.dashboard_workers_allGood()}</span>
    {/if}
  {/snippet}
</DashboardCard>
