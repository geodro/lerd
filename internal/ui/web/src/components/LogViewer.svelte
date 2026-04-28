<script lang="ts">
  import { onDestroy, tick } from 'svelte';
  import { writable } from 'svelte/store';
  import { createLogStream, type LogStream } from '$lib/logStream';
  import { m } from '../paraglide/messages.js';

  interface Props {
    path: string;
    emptyLabel?: string;
    maxLines?: number;
    highlight?: (line: string) => string | null;
  }
  let { path, emptyLabel, maxLines = 500, highlight }: Props = $props();
  const resolvedEmpty = $derived(emptyLabel ?? m.sites_appLogs_waiting());

  let current: LogStream | null = null;
  const lines = writable<string[]>([]);
  const connected = writable<boolean>(false);
  let lineUnsub: (() => void) | null = null;
  let connUnsub: (() => void) | null = null;
  let scrollEl: HTMLDivElement | null = $state(null);

  function bind(stream: LogStream) {
    lineUnsub?.();
    connUnsub?.();
    lineUnsub = stream.lines.subscribe((v) => {
      lines.set(v);
      tick().then(() => {
        if (scrollEl) scrollEl.scrollTop = scrollEl.scrollHeight;
      });
    });
    connUnsub = stream.connected.subscribe((v) => connected.set(v));
  }

  $effect(() => {
    const p = path;
    const max = maxLines;
    current?.close();
    const s = createLogStream(p, max);
    current = s;
    bind(s);
    s.connect();
    return () => {
      s.close();
    };
  });

  onDestroy(() => {
    lineUnsub?.();
    connUnsub?.();
    current?.close();
  });

  function reconnect() {
    current?.connect();
  }
  function clearLines() {
    current?.clear();
  }

  function lineClass(line: string): string {
    if (highlight) {
      const out = highlight(line);
      if (out) return out;
    }
    return 'text-gray-600 dark:text-gray-400';
  }
</script>

<div class="flex-1 flex flex-col overflow-hidden min-h-0">
  <div class="flex items-center justify-between px-3 sm:px-5 py-2 shrink-0">
    <span
      class="flex items-center gap-1.5 text-[10px] {$connected
        ? 'text-emerald-600 dark:text-emerald-500'
        : 'text-gray-400 dark:text-gray-600'}"
    >
      <span
        class="w-1.5 h-1.5 rounded-full {$connected
          ? 'bg-emerald-500 animate-pulse'
          : 'bg-gray-400 dark:bg-gray-600'}"
      ></span>
      {$connected ? m.common_live() : m.common_disconnected()}
    </span>
    <div class="flex items-center gap-2">
      <button
        onclick={clearLines}
        class="text-[10px] text-gray-400 hover:text-gray-600 dark:hover:text-gray-400 transition-colors"
      >{m.common_clear()}</button>
      <button
        onclick={reconnect}
        class="text-xs text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 border border-gray-200 dark:border-lerd-border hover:border-gray-300 dark:hover:border-lerd-muted rounded px-2 py-1 transition-colors"
      >{m.common_reconnect()}</button>
    </div>
  </div>
  <div
    bind:this={scrollEl}
    class="flex-1 overflow-y-auto bg-gray-50 dark:bg-lerd-bg px-4 py-3 font-mono text-[11px] leading-relaxed space-y-0.5"
  >
    {#if $lines.length === 0}
      <div class="text-gray-400 dark:text-gray-700 italic">{resolvedEmpty}</div>
    {:else}
      {#each $lines as line, i (i + ':' + line.slice(0, 20))}
        <div class="whitespace-pre-wrap break-all {lineClass(line)}">{line}</div>
      {/each}
    {/if}
  </div>
</div>
