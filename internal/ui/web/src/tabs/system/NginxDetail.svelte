<script lang="ts">
  import DetailPanel from '$components/DetailPanel.svelte';
  import DetailHeader from '$components/DetailHeader.svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import InfoRow from '$components/InfoRow.svelte';
  import LogViewer from '$components/LogViewer.svelte';
  import { status } from '$stores/status';
  import { m } from '../../paraglide/messages.js';

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
  <div class="px-3 sm:px-5 py-3 shrink-0">
    <InfoRow label={m.system_container()} value="lerd-nginx" />
  </div>
  <LogViewer path="/api/logs/lerd-nginx" {highlight} />
</DetailPanel>
