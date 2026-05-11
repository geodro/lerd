<script lang="ts">
  import { onMount } from 'svelte';
  import DetailPanel from '$components/DetailPanel.svelte';
  import DetailHeader from '$components/DetailHeader.svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import DumpsTab from '$tabs/DumpsTab.svelte';
  import { status as dumpsStatusValue, refreshStatus, toggleDumps, togglePassthrough } from '$stores/dumps';

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
  <div class="px-3 sm:px-5 py-2 space-y-2 shrink-0 text-xs text-gray-500 dark:text-gray-400">
    <p>
      Captures every <code class="bg-gray-100 dark:bg-white/5 px-1 rounded">dump()</code> / <code class="bg-gray-100 dark:bg-white/5 px-1 rounded">dd()</code> call from PHP-FPM and CLI into the dashboard.
      {#if $dumpsStatusValue}
        Listener {$dumpsStatusValue.listening ? 'up' : 'down'} on <code class="bg-gray-100 dark:bg-white/5 px-1 rounded">{$dumpsStatusValue.addr}</code>.
        {#if $dumpsStatusValue.count > 0}
          Buffered: <span class="font-mono">{$dumpsStatusValue.count}</span>{#if $dumpsStatusValue.last_ts}, last {new Date($dumpsStatusValue.last_ts).toLocaleTimeString()}{/if}.
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
        <span>Also print to response (passthrough)</span>
      </label>
      {#if switchingPassthrough}
        <span class="text-[11px] text-amber-600 dark:text-amber-400">Restarting FPM containers…</span>
      {:else}
        <span class="text-[11px] text-gray-400 dark:text-gray-500">Toggling restarts every <code class="font-mono">lerd-php*-fpm</code> unit.</span>
      {/if}
    </div>
  </div>
  <div class="flex-1 min-h-0 overflow-hidden">
    <DumpsTab />
  </div>
</DetailPanel>
