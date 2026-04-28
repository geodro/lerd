<script lang="ts">
  import { routeRest } from '$stores/route';
  import DnsDetail from './system/DnsDetail.svelte';
  import NginxDetail from './system/NginxDetail.svelte';
  import WatcherDetail from './system/WatcherDetail.svelte';
  import PhpDetail from './system/PhpDetail.svelte';
  import NodePage from './system/NodePage.svelte';
  import LerdDetail from './system/LerdDetail.svelte';

  const selected = $derived($routeRest || 'lerd');
  const phpVersion = $derived(selected.startsWith('php-') ? selected.slice(4) : '');
</script>

{#if selected === 'dns'}
  <DnsDetail />
{:else if selected === 'nginx'}
  <NginxDetail />
{:else if selected === 'watcher'}
  <WatcherDetail />
{:else if phpVersion}
  <PhpDetail version={phpVersion} />
{:else if selected === 'node' || selected === 'node-install' || selected.startsWith('node-')}
  <NodePage />
{:else}
  <LerdDetail />
{/if}
