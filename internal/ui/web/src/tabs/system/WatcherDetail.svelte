<script lang="ts">
  import DetailPanel from '$components/DetailPanel.svelte';
  import DetailHeader from '$components/DetailHeader.svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import LogViewer from '$components/LogViewer.svelte';
  import { status, loadStatus } from '$stores/status';
  import { apiFetch } from '$lib/api';
  import { m } from '../../paraglide/messages.js';

  let starting = $state(false);
  async function startWatcher() {
    starting = true;
    try {
      await apiFetch('/api/watcher/start', { method: 'POST' });
      await loadStatus();
    } finally {
      starting = false;
    }
  }
</script>

{#snippet pill()}
  {#if $status.watcher_running}
    <StatusPill tone="ok" label={m.common_running()} />
  {:else}
    <div class="flex items-center gap-2">
      <StatusPill tone="error" label={m.common_stopped()} />
      <DetailButton tone="success" disabled={starting} loading={starting} onclick={startWatcher}>
        {m.common_start()}
      </DetailButton>
    </div>
  {/if}
{/snippet}

<DetailPanel>
  <DetailHeader title={m.system_watcher()} trailing={pill} />
  <p class="px-3 sm:px-5 py-3 text-xs text-gray-400 shrink-0">
    {@html m.system_watcher_description({ env: '<code class="bg-gray-100 dark:bg-white/5 px-1 rounded">LERD_DEBUG=1</code>' })}
  </p>
  <LogViewer path="/api/watcher/logs" emptyLabel={m.system_watcher_quiet()} />
</DetailPanel>
