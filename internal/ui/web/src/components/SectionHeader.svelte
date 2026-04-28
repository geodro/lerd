<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    title: string;
    trailing?: Snippet;
  }
  let { title, trailing }: Props = $props();
</script>

<div
  class="px-3 pt-2.5 pb-1 border-t border-gray-100 dark:border-lerd-border flex items-center gap-2"
>
  <span class="text-[10px] font-semibold text-gray-400 uppercase tracking-wider">{title}</span>
  {#if trailing}{@render trailing()}{/if}
</div>
