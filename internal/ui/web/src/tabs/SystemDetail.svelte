<script lang="ts">
  import { routeRest } from '$stores/route';
  import { status } from '$stores/status';
  import DnsDetail from './system/DnsDetail.svelte';
  import NginxDetail from './system/NginxDetail.svelte';
  import WatcherDetail from './system/WatcherDetail.svelte';
  import DumpBridgeDetail from './system/DumpBridgeDetail.svelte';
  import PhpDetail from './system/PhpDetail.svelte';
  import NodePage from './system/NodePage.svelte';
  import LerdDetail from './system/LerdDetail.svelte';
  import WorkerModeDetail from './system/WorkerModeDetail.svelte';

  const selected = $derived($routeRest || 'lerd');
  const phpVersion = $derived(selected.startsWith('php-') ? selected.slice(4) : '');
  const dnsHidden = $derived($status.dns?.enabled === false);
</script>

{#if selected === 'dns' && !dnsHidden}
  <DnsDetail />
{:else if selected === 'nginx'}
  <NginxDetail />
{:else if selected === 'watcher'}
  <WatcherDetail />
{:else if selected === 'dump-bridge'}
  <DumpBridgeDetail />
{:else if phpVersion}
  <PhpDetail version={phpVersion} />
{:else if selected === 'node' || selected === 'node-install' || selected.startsWith('node-')}
  <NodePage />
{:else if selected === 'workermode'}
  <WorkerModeDetail />
{:else}
  <LerdDetail />
{/if}
