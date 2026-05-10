<script lang="ts">
  import type { DumpEvent } from '$lib/dumpsStream';
  import DumpView from './DumpView.svelte';
  import { parseDump, looksLikeDump } from '$lib/dump-parser';

  interface Props {
    event: DumpEvent;
  }
  let { event }: Props = $props();

  // The CliDumper output (event.text) is parseable into a tree by the
  // existing dump-parser. Fall back to a <pre> block if the text isn't
  // recognisable (defensive: handles future bridge formats).
  const parsed = $derived(() => {
    const text = event.text ?? '';
    if (!looksLikeDump(text)) return null;
    const result = parseDump(text);
    if (!result.ok || result.nodes.length === 0) return null;
    return result.nodes;
  });

  function shortFile(path: string): string {
    if (!path) return '';
    const parts = path.split('/');
    if (parts.length <= 3) return path;
    return '…/' + parts.slice(-3).join('/');
  }

  function timeOnly(ts: string): string {
    const d = new Date(ts);
    if (isNaN(d.getTime())) return ts;
    return d.toLocaleTimeString();
  }
</script>

<div class="rounded border border-gray-200 dark:border-lerd-border p-3 mb-2 bg-white dark:bg-lerd-card text-sm">
  <div class="flex items-baseline gap-2 mb-2 flex-wrap">
    <span class="font-mono text-xs text-gray-500">{timeOnly(event.ts)}</span>
    {#if event.label}
      <span class="font-mono text-amber-700 dark:text-amber-300">{event.label}</span>
    {/if}
    {#if event.src.file}
      <span class="ml-auto font-mono text-xs text-gray-400 truncate" title={event.src.file + ':' + event.src.line}>
        {shortFile(event.src.file)}:{event.src.line}
      </span>
    {/if}
  </div>
  {#if parsed()}
    <div class="font-mono text-xs">
      {#each parsed() as node}
        <DumpView {node} />
      {/each}
    </div>
  {:else}
    <pre class="font-mono text-xs whitespace-pre-wrap text-gray-700 dark:text-gray-200">{event.text ?? ''}</pre>
  {/if}
  {#if event.trunc}
    <div class="text-xs text-amber-600 dark:text-amber-400 mt-2">⚠ truncated by dumper caps</div>
  {/if}
</div>
