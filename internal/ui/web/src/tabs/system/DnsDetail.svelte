<script lang="ts">
  import DetailPanel from '$components/DetailPanel.svelte';
  import DetailHeader from '$components/DetailHeader.svelte';
  import DetailTabs, { type TabItem } from '$components/DetailTabs.svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import InfoRow from '$components/InfoRow.svelte';
  import LogViewer from '$components/LogViewer.svelte';
  import DnsConfigTab from './DnsConfigTab.svelte';
  import { status, dnsState } from '$stores/status';
  import { m } from '../../paraglide/messages.js';

  const logsEnabled = $derived($status.dns?.enabled !== false);

  type TabId = 'logs' | 'config';
  let active = $state<TabId>('logs');
  const tabs: TabItem<TabId>[] = [
    { id: 'logs', label: m.services_tabs_logs() },
    { id: 'config', label: m.services_tabs_tuning() }
  ];
</script>

{#snippet pill()}
  {#if $status.dns?.enabled === false}
    <StatusPill tone="muted" label="disabled" />
  {:else}
    {@const dns = dnsState($status)}
    <StatusPill
      tone={dns === 'ok' ? 'ok' : dns === 'degraded' ? 'warn' : 'error'}
      label={dns === 'ok' ? m.system_dns_ok() : dns === 'degraded' ? m.system_dns_degraded() : m.system_dns_failed()}
    />
  {/if}
{/snippet}

<DetailPanel>
  <DetailHeader title={m.system_dns()} trailing={pill} />
  <DetailTabs {tabs} {active} onchange={(id) => (active = id)} />

  {#if active === 'config'}
    <DnsConfigTab />
  {:else}
    <div class="px-3 py-3 space-y-2 shrink-0">
      <InfoRow label={m.system_tld()} value={'.' + $status.dns.tld} />
      {#if $status.dns?.enabled === false}
        <p class="text-xs text-gray-400">
          lerd-dns is disabled. Sites resolve through the system resolver via *.{$status.dns.tld} (RFC 6761). HTTPS is unavailable in this mode. To re-enable, set <code class="bg-gray-100 dark:bg-white/5 px-1 rounded-sm">dns.enabled: true</code> in <code class="bg-gray-100 dark:bg-white/5 px-1 rounded-sm">~/.config/lerd/config.yaml</code> and run <code class="bg-gray-100 dark:bg-white/5 px-1 rounded-sm">lerd install</code>.
        </p>
      {:else if dnsState($status) === 'degraded'}
        <p class="text-xs text-gray-400">{m.system_dns_degradedHint()}</p>
      {:else if !$status.dns.ok}
        <p class="text-xs text-gray-400">
          {@html m.system_dns_fixHint({
            start: '<strong class="text-gray-500">' + m.common_start() + '</strong>',
            cmd: '<code class="bg-gray-100 dark:bg-white/5 px-1 rounded-sm text-gray-500">lerd install</code>'
          })}
        </p>
      {/if}
    </div>
    {#if logsEnabled}
      <LogViewer
        path="/api/logs/lerd-dns"
        emptyLabel={m.system_dns_quietDefault({ option: '`log-queries`', path: '~/.local/share/lerd/dnsmasq/lerd.conf' })}
      />
    {/if}
  {/if}
</DetailPanel>
