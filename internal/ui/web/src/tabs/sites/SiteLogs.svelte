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
  }
  let { site }: Props = $props();

  type TabId = string;

  const tabs: TabItem<TabId>[] = $derived.by(() => {
    const xs: TabItem<TabId>[] = [];
    if (site.has_app_logs) xs.push({ id: 'app', label: m.sites_tabs_appLogs() });
    xs.push({ id: 'fpm', label: fpmTabLabelI18n(site) });
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

  // If 'app' isn't available (site has no app logs), snap to the first available tab.
  $effect(() => {
    const ids = new Set(tabs.map((t) => t.id));
    if (!ids.has(active)) active = tabs[0]?.id ?? 'fpm';
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
    if (active.startsWith('worker:')) return `/api/worker/${name}/${active.slice(7)}/logs`;
    return '';
  });
</script>

<div class="flex-1 flex flex-col overflow-hidden min-h-0">
  <DetailTabs {tabs} {active} onchange={(id) => (active = id)} />
  {#if active === 'app'}
    {#key site.domain}
      <AppLogsTab {site} />
    {/key}
  {:else if streamPath}
    {#key active + '@' + streamPath}
      <LogViewer path={streamPath} highlight={active === 'fpm' ? fpmHighlight : undefined} />
    {/key}
  {/if}
</div>
