<script lang="ts">
  import { m } from '../paraglide/messages.js';

  interface Props {
    logs: string[];
  }
  let { logs }: Props = $props();

  let scrollEl: HTMLDivElement | null = $state(null);

  // Keep the newest output in view as lines stream in.
  $effect(() => {
    logs.length;
    if (scrollEl) {
      requestAnimationFrame(() => {
        if (scrollEl) scrollEl.scrollTop = scrollEl.scrollHeight;
      });
    }
  });
</script>

<div
  bind:this={scrollEl}
  class="bg-gray-50 dark:bg-black/30 rounded-lg p-3 max-h-72 overflow-y-auto font-mono text-xs text-gray-600 dark:text-gray-400 space-y-0.5"
>
  {#each logs as line, i (i)}
    <div>{line}</div>
  {/each}
  {#if logs.length === 0}
    <div class="text-gray-400 dark:text-gray-500">{m.link_waitingOutput()}</div>
  {/if}
</div>
