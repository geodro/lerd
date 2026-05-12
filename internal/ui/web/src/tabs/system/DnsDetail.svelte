<script lang="ts">
  import DetailPanel from '$components/DetailPanel.svelte';
  import DetailHeader from '$components/DetailHeader.svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import InfoRow from '$components/InfoRow.svelte';
  import LogViewer from '$components/LogViewer.svelte';
  import { status } from '$stores/status';
  import { m } from '../../paraglide/messages.js';
</script>

{#snippet pill()}
  {#if $status.dns?.enabled === false}
    <StatusPill tone="muted" label="disabled" />
  {:else}
    <StatusPill tone={$status.dns.ok ? 'ok' : 'error'} label={$status.dns.ok ? m.system_dns_ok() : m.system_dns_failed()} />
  {/if}
{/snippet}

<DetailPanel>
  <DetailHeader title={m.system_dns()} trailing={pill} />
  <div class="px-3 sm:px-5 py-3 space-y-2 shrink-0">
    <InfoRow label={m.system_tld()} value={'.' + $status.dns.tld} />
    {#if $status.dns?.enabled === false}
      <p class="text-xs text-gray-400">
        lerd-dns is disabled. Sites resolve through the system resolver via *.{$status.dns.tld} (RFC 6761). HTTPS is unavailable in this mode. To re-enable, set <code class="bg-gray-100 dark:bg-white/5 px-1 rounded-sm">dns.enabled: true</code> in <code class="bg-gray-100 dark:bg-white/5 px-1 rounded-sm">~/.config/lerd/config.yaml</code> and run <code class="bg-gray-100 dark:bg-white/5 px-1 rounded-sm">lerd install</code>.
      </p>
    {:else if !$status.dns.ok}
      <p class="text-xs text-gray-400">
        {@html m.system_dns_fixHint({
          start: '<strong class="text-gray-500">' + m.common_start() + '</strong>',
          cmd: '<code class="bg-gray-100 dark:bg-white/5 px-1 rounded-sm text-gray-500">lerd install</code>'
        })}
      </p>
    {/if}
  </div>
  {#if $status.dns?.enabled !== false}
    <LogViewer
      path="/api/logs/lerd-dns"
      emptyLabel={m.system_dns_quietDefault({ option: '`log-queries`', path: '~/.local/share/lerd/dnsmasq/lerd.conf' })}
    />
  {/if}
</DetailPanel>
