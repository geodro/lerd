<script lang="ts">
  import { onMount } from 'svelte';
  import DashboardCard from './DashboardCard.svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import StatusDot from '$components/StatusDot.svelte';
  import { status, lerdStatusColor } from '$stores/status';
  import { sitesByPhp, sitesByNode } from '$stores/sites';
  import { goToTab } from '$stores/route';
  import { m } from '../../paraglide/messages.js';
  import { status as dumpsStatus, refreshStatus as refreshDumpsStatus } from '$stores/dumps';
  import { notifyPrefs, permissionState, autoSubscribeDisabled } from '$lib/notify';

  onMount(() => {
    void refreshDumpsStatus();
  });

  const dumpsBuffered = $derived($dumpsStatus?.count ?? 0);
  const dumpsOn = $derived(Boolean($dumpsStatus?.enabled));
  const notifyOn = $derived(
    $permissionState === 'granted' && !$autoSubscribeDisabled && $notifyPrefs.enabled
  );

  const nodeVersions = $derived.by(() => {
    const entries = [...$sitesByNode.entries()].sort((a, b) => a[0].localeCompare(b[0]));
    return entries;
  });

  const headerTone = $derived.by(() => {
    switch ($lerdStatusColor) {
      case 'green': return { tone: 'ok' as const, label: m.dashboard_health_healthy() };
      case 'yellow': return { tone: 'warn' as const, label: m.dashboard_health_attention() };
      case 'red': return { tone: 'error' as const, label: m.dashboard_health_problem() };
      default: return { tone: 'muted' as const, label: m.dashboard_health_loading() };
    }
  });

  const cardTone = $derived($lerdStatusColor === 'red' ? 'critical' : 'default');
</script>

<DashboardCard title={m.dashboard_health_title()} tone={cardTone}>
  {#snippet badge()}
    <StatusPill tone={headerTone.tone} label={headerTone.label} />
  {/snippet}

  {#if $status.dns?.enabled !== false}
    <div class="flex items-center justify-between text-sm">
      <span class="text-gray-600 dark:text-gray-300">{m.dashboard_health_dns({ tld: $status.dns.tld })}</span>
      <StatusDot color={$status.dns.ok ? 'green' : 'red'} />
    </div>
  {/if}

  <div class="flex items-center justify-between text-sm">
    <span class="text-gray-600 dark:text-gray-300">{m.dashboard_health_nginx()}</span>
    <StatusDot color={$status.nginx.running ? 'green' : 'red'} />
  </div>

  <div class="flex items-center justify-between text-sm">
    <span class="text-gray-600 dark:text-gray-300">{m.dashboard_health_watcher()}</span>
    <StatusDot color={$status.watcher_running ? 'green' : 'red'} />
  </div>

  <div class="flex items-center justify-between text-sm">
    <span class="text-gray-600 dark:text-gray-300">Dump bridge</span>
    <span class="flex items-center gap-1.5">
      {#if dumpsOn && dumpsBuffered > 0}
        <span class="text-[10px] font-mono text-gray-400 dark:text-gray-500">{dumpsBuffered}</span>
      {/if}
      <StatusDot color={dumpsOn ? 'green' : 'gray'} pulse={dumpsOn} />
    </span>
  </div>

  <div class="flex items-center justify-between text-sm">
    <span class="text-gray-600 dark:text-gray-300">{m.notify_settings_title()}</span>
    <StatusDot color={notifyOn ? 'green' : 'red'} />
  </div>

  {#if $status.php_fpms.length > 0}
    <div class="pt-2 border-t border-gray-100 dark:border-lerd-border">
      <div class="text-xs font-semibold text-gray-400 dark:text-gray-500 uppercase tracking-wide mb-2">{m.dashboard_health_php()}</div>
      <div class="flex flex-wrap gap-2">
        {#each $status.php_fpms as fpm (fpm.version)}
          {@const count = $sitesByPhp.get(fpm.version) ?? 0}
          <span class="inline-flex items-center gap-1.5 text-xs font-mono px-2 py-0.5 rounded-sm bg-gray-100 dark:bg-white/5 text-gray-600 dark:text-gray-300">
            <StatusDot color={fpm.running ? 'green' : 'gray'} size="xs" />
            {fpm.version}
            {#if count > 0}
              <span class="text-gray-400 dark:text-gray-500">· {count}</span>
            {/if}
          </span>
        {/each}
      </div>
    </div>
  {/if}

  {#if nodeVersions.length > 0}
    <div class="pt-2 border-t border-gray-100 dark:border-lerd-border">
      <div class="text-xs font-semibold text-gray-400 dark:text-gray-500 uppercase tracking-wide mb-2">{m.dashboard_health_node()}</div>
      <div class="flex flex-wrap gap-2">
        {#each nodeVersions as [version, count] (version)}
          <span class="inline-flex items-center gap-1.5 text-xs font-mono px-2 py-0.5 rounded-sm bg-gray-100 dark:bg-white/5 text-gray-600 dark:text-gray-300">
            <StatusDot color={version === $status.node_default ? 'emerald' : 'gray'} size="xs" />
            {version}
            <span class="text-gray-400 dark:text-gray-500">· {count}</span>
          </span>
        {/each}
      </div>
    </div>
  {/if}

  {#snippet footer()}
    <button
      onclick={() => goToTab('system', 'lerd')}
      class="text-xs font-medium text-lerd-red hover:text-lerd-redhov"
    >{m.dashboard_health_open()}</button>
  {/snippet}
</DashboardCard>
