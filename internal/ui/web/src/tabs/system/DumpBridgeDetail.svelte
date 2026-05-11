<script lang="ts">
  import { onMount } from 'svelte';
  import DetailPanel from '$components/DetailPanel.svelte';
  import DetailHeader from '$components/DetailHeader.svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import DumpsTab from '$tabs/DumpsTab.svelte';
  import { status as dumpsStatusValue, refreshStatus, toggleDumps, togglePassthrough } from '$stores/dumps';
  import { m } from '../../paraglide/messages.js';

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

  let switchingPassthrough = $state(false);
  async function flipPassthrough() {
    if (switchingPassthrough) return;
    switchingPassthrough = true;
    try {
      await togglePassthrough(!$dumpsStatusValue?.passthrough);
      await refreshStatus();
    } finally {
      switchingPassthrough = false;
    }
  }

  onMount(() => {
    void refreshStatus();
  });
</script>

{#snippet pill()}
  {#if $dumpsStatusValue?.enabled}
    <div class="flex items-center gap-2">
      <StatusPill tone="ok" label={m.dumps_bridge_capturing()} />
      <DetailButton tone="secondary" disabled={toggling} loading={toggling} onclick={flip}>
        {m.common_disable()}
      </DetailButton>
    </div>
  {:else}
    <div class="flex items-center gap-2">
      <StatusPill tone="muted" label={m.dumps_bridge_off()} />
      <DetailButton tone="success" disabled={toggling} loading={toggling} onclick={flip}>
        {m.common_enable()}
      </DetailButton>
    </div>
  {/if}
{/snippet}

<DetailPanel>
  <DetailHeader title={m.dumps_bridge_title()} trailing={pill} />
  <div class="px-3 sm:px-5 py-2 space-y-2 shrink-0 text-xs text-gray-500 dark:text-gray-400">
    <p>
      {m.dumps_bridge_description()}
      {#if $dumpsStatusValue}
        {m.dumps_bridge_listener({ state: $dumpsStatusValue.listening ? m.dumps_bridge_listenerUp() : m.dumps_bridge_listenerDown(), addr: $dumpsStatusValue.addr })}
        {#if $dumpsStatusValue.count > 0}
          {m.dumps_bridge_buffered({ count: $dumpsStatusValue.count })}{#if $dumpsStatusValue.last_ts}, {m.dumps_bridge_bufferedLast({ time: new Date($dumpsStatusValue.last_ts).toLocaleTimeString() })}{/if}.
        {/if}
      {/if}
    </p>
    <div class="flex items-center gap-2 flex-wrap">
      <label class="inline-flex items-center gap-2 cursor-pointer select-none">
        <input
          type="checkbox"
          class="rounded border-gray-300 dark:border-lerd-border bg-white dark:bg-lerd-card text-lerd-red focus:ring-lerd-red"
          checked={Boolean($dumpsStatusValue?.passthrough)}
          disabled={switchingPassthrough}
          onchange={flipPassthrough}
        />
        <span>{m.dumps_bridge_passthrough()}</span>
      </label>
      {#if switchingPassthrough}
        <span class="text-[11px] text-amber-600 dark:text-amber-400">{m.dumps_bridge_passthroughRestarting()}</span>
      {:else}
        <span class="text-[11px] text-gray-400 dark:text-gray-500">{m.dumps_bridge_passthroughHint()}</span>
      {/if}
    </div>
  </div>
  <div class="flex-1 min-h-0 overflow-hidden">
    <DumpsTab />
  </div>
</DetailPanel>
