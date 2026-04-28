<script lang="ts">
  import DetailPanel from '$components/DetailPanel.svelte';
  import LogViewer from '$components/LogViewer.svelte';
  import DetailTabs, { type TabItem } from '$components/DetailTabs.svelte';
  import ServiceHeader from './ServiceHeader.svelte';
  import ServiceEnvTab from './ServiceEnvTab.svelte';
  import PresetSuggestionBanner from './PresetSuggestionBanner.svelte';
  import type { Service } from '$stores/services';

  interface Props {
    svc: Service;
  }
  let { svc }: Props = $props();

  type TabId = 'logs' | 'env';
  let active = $state<TabId>('logs');

  const hasEnv = $derived(Boolean(svc.env_vars && Object.keys(svc.env_vars).length > 0));
  const tabs = $derived<TabItem<TabId>[]>([
    { id: 'logs', label: 'Logs' },
    { id: 'env', label: '.env', hidden: !hasEnv }
  ]);

  const logPath = $derived.by(() => {
    if (svc.queue_site) return `/api/queue/${svc.queue_site}/logs`;
    if (svc.horizon_site) return `/api/horizon/${svc.horizon_site}/logs`;
    if (svc.stripe_listener_site) return `/api/stripe/${svc.stripe_listener_site}/logs`;
    if (svc.schedule_worker_site) return `/api/schedule/${svc.schedule_worker_site}/logs`;
    if (svc.reverb_site) return `/api/reverb/${svc.reverb_site}/logs`;
    if (svc.worker_site && svc.worker_name) return `/api/worker/${svc.worker_site}/${svc.worker_name}/logs`;
    return `/api/logs/lerd-${svc.name}`;
  });

  function highlight(line: string): string | null {
    if (/ERROR|Error/.test(line)) return 'text-red-500';
    if (/WARNING|Warning/.test(line)) return 'text-yellow-600 dark:text-yellow-400';
    return null;
  }
</script>

<DetailPanel>
  <ServiceHeader {svc} />
  <PresetSuggestionBanner {svc} />
  <DetailTabs {tabs} {active} onchange={(id) => (active = id)} />
  {#if active === 'logs'}
    {#key svc.name + ':' + logPath}
      <LogViewer path={logPath} {highlight} />
    {/key}
  {:else if active === 'env'}
    <ServiceEnvTab {svc} />
  {/if}
</DetailPanel>
