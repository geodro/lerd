<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    title: string;
    hint?: Snippet;
    size?: 'sm' | 'md';
  }
  let { title, hint, size = 'md' }: Props = $props();

  const padY = $derived(size === 'sm' ? 'py-6' : 'py-10');
  const titleSize = $derived(size === 'sm' ? 'text-[11px]' : 'text-sm');
</script>

<div class="px-4 {padY} text-center space-y-1">
  <p class="{titleSize} text-gray-500 dark:text-gray-400">{title}</p>
  {#if hint}
    <div class="text-[11px] text-gray-400 dark:text-gray-500">{@render hint()}</div>
  {/if}
</div>
