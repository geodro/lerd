<script lang="ts">
  import ListPanel from '$components/ListPanel.svelte';
  import ActionButton from '$components/ActionButton.svelte';
  import Icon from '$components/Icon.svelte';
  import ListRow from '$components/ListRow.svelte';
  import StatusDot from '$components/StatusDot.svelte';
  import LoadingRow from '$components/LoadingRow.svelte';
  import { routeRest, goToTab } from '$stores/route';
  import { status, statusLoaded, lerdStatusColor, allCoreRunning } from '$stores/status';
  import { phpVersions } from '$stores/phpVersions';
  import { nodeVersions } from '$stores/nodeVersions';
  import { sitesByNode } from '$stores/sites';
  import { version } from '$stores/version';
  import { accessMode } from '$stores/accessMode';
  import { lerdStart, lerdStop, lerdStarting, lerdStopping } from '$stores/lerdLifecycle';
  import { workerExecMode, workerModeApplies, loadWorkerMode } from '$stores/workerMode';
  import { status as dumpsStatusValue, refreshStatus as refreshDumpsStatus } from '$stores/dumps';
  import { notifyPrefs, permissionState, autoSubscribeDisabled } from '$lib/notify';
  import { onMount } from 'svelte';
  import { m } from '../paraglide/messages.js';

  onMount(() => {
    loadWorkerMode();
    void refreshDumpsStatus();
  });

  const selected = $derived($routeRest || 'lerd');
  const notifyEffectiveOn = $derived(
    $permissionState === 'granted' && !$autoSubscribeDisabled && $notifyPrefs.enabled
  );

  function select(id: string) {
    goToTab('system', id);
  }
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
    {#if $status.dns?.enabled !== false}
      {#snippet dnsDot()}<StatusDot color={$status.dns.ok ? 'green' : 'red'} />{/snippet}
      <ListRow active={selected === 'dns'} onclick={() => select('dns')} leading={dnsDot}>{m.system_dns()}</ListRow>
    {/if}

    {#snippet nginxDot()}<StatusDot color={$status.nginx.running ? 'green' : 'gray'} />{/snippet}
    <ListRow active={selected === 'nginx'} onclick={() => select('nginx')} leading={nginxDot}>{m.system_nginx()}</ListRow>

    {#if $phpVersions.length > 0}
      {@const phpSelected = selected === 'php' || selected.startsWith('php-')}
      {@const anyFpmRunning = $status.php_fpms.some((f) => f.running)}
      {#snippet phpLeading()}<StatusDot color={anyFpmRunning ? 'green' : 'gray'} />{/snippet}
      {#snippet phpTrailing()}
        <span class="text-[10px] font-medium tabular-nums shrink-0 {phpSelected ? 'text-lerd-red/70' : 'text-gray-400 dark:text-gray-600'}">{$phpVersions.length}</span>
      {/snippet}
      <ListRow active={phpSelected} onclick={() => select('php')} leading={phpLeading} trailing={phpTrailing}>PHP</ListRow>
    {/if}

    {#snippet nodeLeading()}<StatusDot color={$status.using_system_bun ? 'amber' : $status.node_managed_by_lerd ? 'green' : 'blue'} />{/snippet}
    {#snippet nodeTrailing()}
      {#if $status.using_system_bun}
        <span class="text-[10px] font-medium shrink-0 text-amber-600 dark:text-amber-400">🥟 {$status.bun_version}</span>
      {:else}
        <span class="text-[10px] font-medium tabular-nums shrink-0 {selected === 'node' ? 'text-lerd-red/70' : 'text-gray-400 dark:text-gray-600'}">{$nodeVersions.length}</span>
      {/if}
    {/snippet}
    <ListRow active={selected === 'node'} onclick={() => select('node')} leading={nodeLeading} trailing={nodeTrailing}>
      {$status.using_system_bun ? m.dashboard_health_jsRuntime() : m.system_nodeJs()}
    </ListRow>

    {#snippet notifyDot()}<StatusDot color={notifyEffectiveOn ? 'green' : 'red'} />{/snippet}
    <ListRow active={selected === 'notifications'} onclick={() => select('notifications')} leading={notifyDot}>
      {m.notify_settings_title()}
    </ListRow>

    {#snippet dumpBridgeDot()}<StatusDot color={$dumpsStatusValue?.enabled ? 'green' : 'gray'} pulse={Boolean($dumpsStatusValue?.enabled)} />{/snippet}
    {#snippet dumpBridgeTrailing()}
      {#if $dumpsStatusValue?.enabled && ($dumpsStatusValue?.count ?? 0) > 0}
        <span class="text-[10px] font-medium tabular-nums shrink-0 {selected === 'dump-bridge' ? 'text-lerd-red/70' : 'text-gray-400 dark:text-gray-600'}">{$dumpsStatusValue.count}</span>
      {/if}
    {/snippet}
    <ListRow active={selected === 'dump-bridge'} onclick={() => select('dump-bridge')} leading={dumpBridgeDot} trailing={dumpBridgeTrailing}>{m.debug_title()}</ListRow>

    {#if $workerModeApplies}
      {#snippet workerModeDot()}<StatusDot color={$workerExecMode === 'container' ? 'sky' : 'emerald'} />{/snippet}
      <ListRow active={selected === 'workermode'} onclick={() => select('workermode')} leading={workerModeDot}>
        {m.system_workerMode_listLabel()}
      </ListRow>
    {/if}

    {#snippet watcherDot()}<StatusDot color={$status.watcher_running ? 'green' : 'gray'} />{/snippet}
    <ListRow active={selected === 'watcher'} onclick={() => select('watcher')} leading={watcherDot}>{m.system_watcher()}</ListRow>

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
