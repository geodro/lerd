<script lang="ts">
  import ListPanel from '$components/ListPanel.svelte';
  import ActionButton from '$components/ActionButton.svelte';
  import Icon from '$components/Icon.svelte';
  import ListRow from '$components/ListRow.svelte';
  import StatusDot from '$components/StatusDot.svelte';
  import SectionHeader from '$components/SectionHeader.svelte';
  import LoadingRow from '$components/LoadingRow.svelte';
  import { routeRest, goToTab } from '$stores/route';
  import { status, statusLoaded, lerdStatusColor, fpmRunning, allCoreRunning } from '$stores/status';
  import { phpVersions } from '$stores/phpVersions';
  import { nodeVersions } from '$stores/nodeVersions';
  import { sitesByPhp, sitesByNode } from '$stores/sites';
  import { version } from '$stores/version';
  import { accessMode } from '$stores/accessMode';
  import { lerdStart, lerdStop, lerdStarting, lerdStopping } from '$stores/lerdLifecycle';
  import { m } from '../paraglide/messages.js';

  const selected = $derived($routeRest || 'lerd');

  function select(id: string) {
    goToTab('system', id);
  }

  const phpSiteCount = $derived((v: string) => $sitesByPhp.get(v) ?? 0);
</script>

{#snippet actions()}
  {#if $accessMode.loopback && !$allCoreRunning}
    <ActionButton
      title={m.system_startLerd()}
      tone="success"
      onclick={lerdStart}
      disabled={$lerdStarting || $lerdStopping}
      loading={$lerdStarting}
    >
      <Icon name="play" class="w-3.5 h-3.5" />
    </ActionButton>
  {/if}
  {#if $accessMode.loopback}
    <ActionButton
      title={m.system_stopLerd()}
      onclick={lerdStop}
      disabled={$lerdStarting || $lerdStopping}
      loading={$lerdStopping}
    >
      <Icon name="stop" class="w-3.5 h-3.5" />
    </ActionButton>
  {/if}
{/snippet}

<ListPanel title={m.system_title()} {actions}>
  {#if !$statusLoaded}
    <LoadingRow />
  {:else}
    {#snippet dnsDot()}<StatusDot color={$status.dns.ok ? 'green' : 'red'} />{/snippet}
    <ListRow active={selected === 'dns'} onclick={() => select('dns')} leading={dnsDot}>{m.system_dns()}</ListRow>

    {#snippet nginxDot()}<StatusDot color={$status.nginx.running ? 'green' : 'gray'} />{/snippet}
    <ListRow active={selected === 'nginx'} onclick={() => select('nginx')} leading={nginxDot}>{m.system_nginx()}</ListRow>

    {#snippet watcherDot()}<StatusDot color={$status.watcher_running ? 'green' : 'gray'} />{/snippet}
    <ListRow active={selected === 'watcher'} onclick={() => select('watcher')} leading={watcherDot}>{m.system_watcher()}</ListRow>

    {#if $phpVersions.length > 0}
      <SectionHeader title={m.system_phpFpm()} />
      {#each $phpVersions as v (v)}
        {@const id = 'php-' + v}
        {#snippet leading()}<StatusDot color={fpmRunning(v) ? 'green' : 'gray'} />{/snippet}
        {#snippet trailing()}
          {#if $status.php_default === v}
            <span class="text-[10px] font-medium shrink-0 {selected === id ? 'text-lerd-red/60' : 'text-gray-400 dark:text-gray-600'}">{m.common_default()}</span>
          {/if}
          {#if phpSiteCount(v) > 0}
            <span class="text-[10px] font-medium tabular-nums shrink-0 ml-1 {selected === id ? 'text-lerd-red/70' : 'text-gray-400 dark:text-gray-600'}">{phpSiteCount(v)}</span>
          {/if}
        {/snippet}
        <ListRow active={selected === id} onclick={() => select(id)} {leading} {trailing}>PHP {v}</ListRow>
      {/each}
    {/if}

    {#snippet nodeLeading()}<StatusDot color={$status.node_managed_by_lerd ? 'green' : 'blue'} />{/snippet}
    {#snippet nodeTrailing()}
      <span class="text-[10px] font-medium tabular-nums shrink-0 {selected === 'node' ? 'text-lerd-red/70' : 'text-gray-400 dark:text-gray-600'}">{$nodeVersions.length}</span>
    {/snippet}
    <ListRow active={selected === 'node'} onclick={() => select('node')} leading={nodeLeading} trailing={nodeTrailing}>
      {m.system_nodeJs()}
    </ListRow>

    {#snippet lerdLeading()}<StatusDot color={$lerdStatusColor} />{/snippet}
    {#snippet lerdTrailing()}
      {#if $version.hasUpdate}
        <span class="ml-auto text-xs font-medium text-yellow-600 dark:text-yellow-400">{m.system_lerd_updateTag()}</span>
      {/if}
    {/snippet}
    <ListRow active={selected === 'lerd'} onclick={() => select('lerd')} leading={lerdLeading} trailing={lerdTrailing}>
      {m.system_lerd()}
    </ListRow>
  {/if}
</ListPanel>
