<script lang="ts">
  import DetailPanel from '$components/DetailPanel.svelte';
  import DetailHeader from '$components/DetailHeader.svelte';
  import DetailTabs, { type TabItem } from '$components/DetailTabs.svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import LogViewer from '$components/LogViewer.svelte';
  import NginxConfigTab from './NginxConfigTab.svelte';
  import { status } from '$stores/status';
  import { m } from '../../paraglide/messages.js';

  type TabId = 'logs' | 'config';
  let active = $state<TabId>('logs');
  const tabs: TabItem<TabId>[] = [
    { id: 'logs', label: m.services_tabs_logs() },
    { id: 'config', label: m.services_tabs_tuning() }
  ];

  function highlight(line: string): string | null {
    if (/error|Error|crit/.test(line)) return 'text-red-500';
    if (/warn/.test(line)) return 'text-yellow-600 dark:text-yellow-400';
    return null;
  }
</script>

{#snippet pill()}
  <StatusPill
    tone={$status.nginx.running ? 'ok' : 'error'}
    label={$status.nginx.running ? m.common_running() : m.common_stopped()}
  />
{/snippet}

<DetailPanel>
  <DetailHeader title={m.system_nginx()} trailing={pill} />
  <DetailTabs {tabs} {active} onchange={(id) => (active = id)} />
  {#if active === 'config'}
    <NginxConfigTab />
  {:else if active === 'logs'}
    <LogViewer path="/api/logs/lerd-nginx" {highlight} />
  {/if}
</DetailPanel>
