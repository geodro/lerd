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

  // A suspended worker's unit is stopped, so it drops out of /api/services
  // entirely. To keep it visible (asleep) instead of silently vanishing, we
  // re-synthesize one entry per site/worktree suspended worker from the sites
  // store and merge it into its group alongside the running ones.
  interface AsleepItem {
    id: string;
    label: string;
    site: string;
  }
  interface MergedGroup {
    key: string;
    label: string;
    running: Service[];
    asleep: AsleepItem[];
  }

  const WORKER_LABELS: Record<string, string> = {
    queue: 'Queues',
    horizon: 'Horizon',
    schedule: 'Schedules',
    reverb: 'Reverb',
    stripe: 'Stripe',
    vite: 'Vite'
  };
  const groupLabelFor = (key: string) =>
    WORKER_LABELS[key] || key.charAt(0).toUpperCase() + key.slice(1);

  const groups = $derived.by((): MergedGroup[] => {
    const map = new Map<string, MergedGroup>();
    for (const g of $workerGroups) {
      map.set(g.key, { key: g.key, label: g.label, running: g.items, asleep: [] });
    }
    const addAsleep = (worker: string, label: string, site: string) => {
      let g = map.get(worker);
      if (!g) {
        g = { key: worker, label: groupLabelFor(worker), running: [], asleep: [] };
        map.set(worker, g);
      }
      g.asleep.push({ id: worker + ':' + label, label, site });
    };
    for (const s of $sites) {
      const name = s.name;
      if (!name) continue;
      for (const w of s.idle_suspended_workers || []) addAsleep(w, name, name);
      for (const wt of s.worktrees || []) {
        for (const w of wt.idle_suspended_workers || [])
          addAsleep(w, name + '/' + (wt.branch || ''), name);
      }
    }
    return [...map.values()].sort((a, b) => a.label.localeCompare(b.label));
  });

  const totalUnits = $derived(groups.reduce((n, g) => n + g.running.length + g.asleep.length, 0));
  const totalActive = $derived(
    groups.reduce((n, g) => n + g.running.filter((i) => i.status === 'active').length, 0)
  );
  const asleepCount = $derived(groups.reduce((n, g) => n + g.asleep.length, 0));
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

  function jumpToSiteName(name: string) {
    const site = get(sites).find((x) => x.name === name);
    if (site && site.domain) goToTab('sites', site.domain);
  }

  async function onHeal() {
    const r = await healAll();
    await loadWorkerHealth();
    if (!r.ok && r.error) console.error('[lerd] heal failed:', r.error);
  }
</script>

<DashboardCard title={m.dashboard_workers_title()} tone={failingCount > 0 ? 'critical' : 'default'}>
  {#snippet badge()}
    <div class="flex items-center gap-1.5">
      {#if failingCount > 0}
        <StatusPill tone="error" label={m.dashboard_workers_failing({ count: failingCount })} />
      {:else if totalUnits > 0}
        <StatusPill tone="ok" label={m.dashboard_workers_summary({ active: totalActive, total: totalUnits })} />
        {#if asleepCount > 0}
          <StatusPill tone="muted" label={m.dashboard_workers_asleep({ count: asleepCount, total: totalUnits })} />
        {/if}
      {:else}
        <StatusPill tone="muted" label={m.dashboard_workers_none()} />
      {/if}
    </div>
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
        {@const activeN = g.running.filter((i) => i.status === 'active').length}
        {@const up = activeN + g.asleep.length}
        {@const total = g.running.length + g.asleep.length}
        <div>
          <div class="flex items-center justify-between text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
            <span>{g.label}</span>
            <span class="font-mono tabular-nums {up === total ? 'text-emerald-600 dark:text-emerald-500' : 'text-gray-500 dark:text-gray-400'}">{up}/{total}</span>
          </div>
          <div class="mt-1 space-y-0.5">
            {#each g.running as item (item.name)}
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
            {#each g.asleep as item (item.id)}
              <button
                type="button"
                onclick={() => jumpToSiteName(item.site)}
                class="group w-full flex items-center gap-2 px-1 py-0.5 -mx-1 rounded-sm text-left text-xs hover:bg-gray-50 dark:hover:bg-white/3 transition-colors"
              >
                <svg class="w-3 h-3 shrink-0 text-sky-500 dark:text-sky-400" viewBox="0 0 24 24" fill="currentColor" aria-label={m.sites_idle()}>
                  <path d="M21.752 15.002A9.72 9.72 0 0 1 18 15.75c-5.385 0-9.75-4.365-9.75-9.75 0-1.33.266-2.597.748-3.752A9.753 9.753 0 0 0 3 11.25C3 16.635 7.365 21 12.75 21a9.753 9.753 0 0 0 9.002-5.998z" />
                </svg>
                <span class="flex-1 truncate text-sky-600 dark:text-sky-400 group-hover:text-lerd-red transition-colors">{item.label}</span>
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
