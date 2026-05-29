<script lang="ts">
  import LogViewer from '$components/LogViewer.svelte';
  import DetailTabs, { type TabItem } from '$components/DetailTabs.svelte';
  import AppLogsTab from './AppLogsTab.svelte';
  import { type Site, fpmContainer } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  function fpmTabLabelI18n(site: Site): string {
    if (site.custom_container) return m.sites_tabs_container();
    if (site.runtime === 'frankenphp') return m.sites_tabs_frankenphp();
    return m.sites_tabs_phpFpm();
  }

  interface Props {
    site: Site;
    activeWorktreeBranch?: string;
  }
  let { site, activeWorktreeBranch = '' }: Props = $props();

  type TabId = string;

  const activeWorktree = $derived.by(() => {
    if (!activeWorktreeBranch) return undefined;
    return (site.worktrees || []).find((w) => w.branch === activeWorktreeBranch);
  });

  const tabs: TabItem<TabId>[] = $derived.by(() => {
    const xs: TabItem<TabId>[] = [];
    if (site.has_app_logs) xs.push({ id: 'app', label: m.sites_tabs_appLogs() });
    // Static sites have no PHP-FPM (or container) runtime, so skip the runtime
    // log tab entirely; only PHP sites and custom containers have one.
    if (site.uses_php || site.custom_container) xs.push({ id: 'fpm', label: fpmTabLabelI18n(site) });
    if (activeWorktreeBranch) {
      // Shared queue/horizon/stripe/schedule/reverb run against main and
      // their journals don't filter per worktree, so drop them here to
      // avoid implying isolation. Per-worktree host workers (vite, etc.)
      // do have their own launchd units — surface those off the
      // worktree's own framework_workers list.
      for (const w of activeWorktree?.framework_workers || []) {
        if (w.running || w.failing) xs.push({ id: 'worker:' + w.name, label: (w.label || w.name) + (w.failing ? ' !' : '') });
      }
      return xs;
    }
    if (site.queue_running || site.queue_failing) xs.push({ id: 'queue', label: m.sites_tabs_queue() + (site.queue_failing ? ' !' : '') });
    if (site.horizon_running || site.horizon_failing) xs.push({ id: 'horizon', label: m.sites_tabs_horizon() + (site.horizon_failing ? ' !' : '') });
    if (site.stripe_running) xs.push({ id: 'stripe', label: m.sites_tabs_stripe() });
    if (site.schedule_running || site.schedule_failing) xs.push({ id: 'schedule', label: m.sites_tabs_schedule() + (site.schedule_failing ? ' !' : '') });
    if (site.reverb_running || site.reverb_failing) xs.push({ id: 'reverb', label: m.sites_tabs_reverb() + (site.reverb_failing ? ' !' : '') });
    for (const w of site.framework_workers || []) {
      if (w.running || w.failing) xs.push({ id: 'worker:' + w.name, label: (w.label || w.name) + (w.failing ? ' !' : '') });
    }
    return xs;
  });

  let active = $state<TabId>('app');

  // If the active tab isn't available, snap to the first one. Falling back to
  // '' (not 'fpm') matters for static sites with no tabs: defaulting to 'fpm'
  // would stream the shared FPM container's logs even though the tab is hidden.
  $effect(() => {
    const ids = new Set(tabs.map((t) => t.id));
    if (!ids.has(active)) active = tabs[0]?.id ?? '';
  });

  const name = $derived(site.name || site.domain);

  function fpmHighlight(line: string): string | null {
    if (/ERROR|Error|PHP Fatal|PHP Warning/.test(line)) return 'text-red-500';
    if (/WARNING|Warning|PHP Notice/.test(line)) return 'text-yellow-600 dark:text-yellow-400';
    return null;
  }

  const streamPath = $derived.by(() => {
    if (active === 'fpm') {
      const c = fpmContainer(site);
      return c ? '/api/logs/' + c : '';
    }
    if (active === 'queue') return `/api/queue/${name}/logs`;
    if (active === 'horizon') return `/api/horizon/${name}/logs`;
    if (active === 'stripe') return `/api/stripe/${name}/logs`;
    if (active === 'schedule') return `/api/schedule/${name}/logs`;
    if (active === 'reverb') return `/api/reverb/${name}/logs`;
    if (active.startsWith('worker:')) {
      const workerName = active.slice(7);
      // Per-worktree units live under lerd-<worker>-<site>-<wtBase>;
      // the backend handler builds the unit from <site>/<worker> in the
      // path, so concat the worktree dir's basename onto the site slug.
      if (activeWorktree?.path) {
        const base = activeWorktree.path.split('/').pop();
        if (base) return `/api/worker/${name}-${base}/${workerName}/logs`;
      }
      return `/api/worker/${name}/${workerName}/logs`;
    }
    return '';
  });
</script>

<div class="flex-1 flex flex-col overflow-hidden min-h-0">
  <DetailTabs {tabs} {active} onchange={(id) => (active = id)} />
  {#if active === 'app' && site.has_app_logs}
    {#key site.domain + '@' + activeWorktreeBranch}
      <AppLogsTab {site} branch={activeWorktreeBranch} />
    {/key}
  {:else if streamPath}
    {#key active + '@' + streamPath}
      <LogViewer path={streamPath} highlight={active === 'fpm' ? fpmHighlight : undefined} />
    {/key}
  {:else}
    <div class="flex-1 flex items-center justify-center text-xs text-gray-400 dark:text-gray-500">
      {m.sites_appLogs_empty()}
    </div>
  {/if}
</div>
