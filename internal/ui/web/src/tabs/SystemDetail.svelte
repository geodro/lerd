<script lang="ts">
  import { routeRest } from '$stores/route';
  import { status } from '$stores/status';
  import DnsDetail from './system/DnsDetail.svelte';
  import NginxDetail from './system/NginxDetail.svelte';
  import WatcherDetail from './system/WatcherDetail.svelte';
  import DumpBridgeDetail from './system/DumpBridgeDetail.svelte';
  import NotificationsDetail from './system/NotificationsDetail.svelte';
  import PhpDetail from './system/PhpDetail.svelte';
  import PhpInstallDetail from './system/PhpInstallDetail.svelte';
  import DebugDetail from './system/DebugDetail.svelte';
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
{:else if selected === 'notifications'}
  <NotificationsDetail />
{:else if selected === 'php-install'}
  <PhpInstallDetail />
{:else if phpVersion}
  <PhpDetail version={phpVersion} />
{:else if selected === 'debug'}
  <DebugDetail />
{:else if selected === 'node' || selected === 'node-install' || selected.startsWith('node-')}
  <NodePage />
{:else if selected === 'workermode'}
  <WorkerModeDetail />
{:else}
  <LerdDetail />
{/if}
