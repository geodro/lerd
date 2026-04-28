<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    title: string;
    trailing?: Snippet;
  }
  let { title, trailing }: Props = $props();
</script>

<div
  class="flex flex-wrap items-center justify-between gap-y-2 px-3 sm:px-5 py-4 border-b border-gray-100 dark:border-lerd-border shrink-0"
>
  <span class="font-semibold text-gray-900 dark:text-white text-base">{title}</span>
  {#if trailing}{@render trailing()}{/if}
</div>
