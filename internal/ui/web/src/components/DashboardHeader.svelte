<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    title: string;
    stats?: Snippet;
  }
  let { title, stats }: Props = $props();
</script>

<div class="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 px-4 py-3 border-b border-gray-100 dark:border-lerd-border">
  <h1 class="text-base font-semibold text-gray-900 dark:text-white">{title}</h1>
  {#if stats}
    <div class="flex items-center gap-4 text-xs text-gray-500 dark:text-gray-400">
      {@render stats()}
    </div>
  {/if}
</div>
