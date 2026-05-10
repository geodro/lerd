<script lang="ts">
  import { onMount } from 'svelte';
  import DetailPanel from '$components/DetailPanel.svelte';
  import DetailHeader from '$components/DetailHeader.svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import DumpsTab from '$tabs/DumpsTab.svelte';
  import { status as dumpsStatusValue, refreshStatus, toggleDumps } from '$stores/dumps';

  let toggling = $state(false);
  async function flip() {
    if (toggling) return;
    toggling = true;
    try {
      await toggleDumps(!$dumpsStatusValue?.enabled);
      await refreshStatus();
    } finally {
      toggling = false;
    }
  }

  onMount(() => {
    void refreshStatus();
  });
</script>

{#snippet pill()}
  {#if $dumpsStatusValue?.enabled}
    <div class="flex items-center gap-2">
      <StatusPill tone="ok" label="Capturing" />
      <DetailButton tone="secondary" disabled={toggling} loading={toggling} onclick={flip}>
        Disable
      </DetailButton>
    </div>
  {:else}
    <div class="flex items-center gap-2">
      <StatusPill tone="muted" label="Off" />
      <DetailButton tone="success" disabled={toggling} loading={toggling} onclick={flip}>
        Enable
      </DetailButton>
    </div>
  {/if}
{/snippet}

<DetailPanel>
  <DetailHeader title="Dump bridge" trailing={pill} />
  <p class="px-3 sm:px-5 py-2 text-xs text-gray-400 shrink-0">
    Captures every <code class="bg-gray-100 dark:bg-white/5 px-1 rounded">dump()</code> / <code class="bg-gray-100 dark:bg-white/5 px-1 rounded">dd()</code> call from your PHP-FPM and CLI contexts into the dashboard.
    {#if $dumpsStatusValue}
      Listener {$dumpsStatusValue.listening ? 'up' : 'down'} on <code class="bg-gray-100 dark:bg-white/5 px-1 rounded">{$dumpsStatusValue.addr}</code>.
      {#if $dumpsStatusValue.count > 0}
        Buffered: <span class="font-mono">{$dumpsStatusValue.count}</span>{#if $dumpsStatusValue.last_ts}, last {new Date($dumpsStatusValue.last_ts).toLocaleTimeString()}{/if}.
      {/if}
    {/if}
  </p>
  <div class="flex-1 min-h-0 overflow-hidden">
    <DumpsTab />
  </div>
</DetailPanel>
