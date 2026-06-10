<script lang="ts">
  import DashboardCard from './DashboardCard.svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import StatusDot from '$components/StatusDot.svelte';
  import DumpBridgeToggle from '$components/DumpBridgeToggle.svelte';
  import ProfilerToggle from '$components/ProfilerToggle.svelte';
  import NotificationsToggle from '$components/NotificationsToggle.svelte';
  import { status, lerdStatusColor, dnsState } from '$stores/status';
  import { sitesByPhp, sitesByNode } from '$stores/sites';
  import { goToTab } from '$stores/route';
  import { m } from '../../paraglide/messages.js';
  import { status as dumpsStatus } from '$stores/dumps';

  const dumpsBuffered = $derived($dumpsStatus?.count ?? 0);
  const dumpsOn = $derived(Boolean($dumpsStatus?.enabled));

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
    {@const dns = dnsState($status)}
    <div class="flex items-center justify-between text-sm">
      <span class="text-gray-600 dark:text-gray-300">{m.dashboard_health_dns({ tld: $status.dns.tld })}</span>
      <span class="inline-flex w-6 h-6 items-center justify-center shrink-0">
        <StatusDot color={dns === 'ok' ? 'green' : dns === 'degraded' ? 'yellow' : 'red'} />
      </span>
    </div>
  {/if}

  <div class="flex items-center justify-between text-sm">
    <span class="text-gray-600 dark:text-gray-300">{m.dashboard_health_nginx()}</span>
    <span class="inline-flex w-6 h-6 items-center justify-center shrink-0">
      <StatusDot color={$status.nginx.running ? 'green' : 'red'} />
    </span>
  </div>

  <div class="flex items-center justify-between text-sm">
    <span class="text-gray-600 dark:text-gray-300">{m.dashboard_health_watcher()}</span>
    <span class="inline-flex w-6 h-6 items-center justify-center shrink-0">
      <StatusDot color={$status.watcher_running ? 'green' : 'red'} />
    </span>
  </div>

  <div class="flex items-center justify-between text-sm">
    <span class="text-gray-600 dark:text-gray-300">{m.dashboard_health_dumpBridge()}</span>
    <span class="flex items-center gap-1.5">
      {#if dumpsOn && dumpsBuffered > 0}
        <span class="text-[10px] font-mono text-gray-400 dark:text-gray-500">{dumpsBuffered}</span>
      {/if}
      <DumpBridgeToggle />
    </span>
  </div>

  <div class="flex items-center justify-between text-sm">
    <span class="text-gray-600 dark:text-gray-300">{m.dashboard_health_profiler()}</span>
    <ProfilerToggle />
  </div>

  <div class="flex items-center justify-between text-sm">
    <span class="text-gray-600 dark:text-gray-300">{m.notify_settings_title()}</span>
    <NotificationsToggle />
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

  {#if nodeVersions.length > 0 || $status.bun_available}
    <div class="pt-2 border-t border-gray-100 dark:border-lerd-border">
      <div class="text-xs font-semibold text-gray-400 dark:text-gray-500 uppercase tracking-wide mb-2">{$status.using_system_bun ? m.dashboard_health_jsRuntime() : m.dashboard_health_node()}</div>
      <div class="flex flex-wrap gap-2">
        {#if !$status.using_system_bun}
          {#each nodeVersions as [version, count] (version)}
            <span class="inline-flex items-center gap-1.5 text-xs font-mono px-2 py-0.5 rounded-sm bg-gray-100 dark:bg-white/5 text-gray-600 dark:text-gray-300">
              <StatusDot color={version === $status.node_default ? 'emerald' : 'gray'} size="xs" />
              {version}
              <span class="text-gray-400 dark:text-gray-500">· {count}</span>
            </span>
          {/each}
        {/if}
        {#if $status.bun_available}
          <span
            class="inline-flex items-center gap-1 text-xs font-mono px-2 py-0.5 rounded-sm
              {$status.using_system_bun
                ? 'bg-amber-50 dark:bg-amber-500/10 text-amber-700 dark:text-amber-300'
                : 'bg-gray-100 dark:bg-white/5 text-gray-600 dark:text-gray-300'}"
            title={$status.using_system_bun ? m.system_node_usingBunHint() : ''}
          >🥟 bun {$status.bun_version}</span>
        {/if}
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
