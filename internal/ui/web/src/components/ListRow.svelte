<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    active?: boolean;
    onclick?: (e: MouseEvent) => void;
    leading?: Snippet;
    trailing?: Snippet;
    children: Snippet;
  }
  let { active = false, onclick, leading, trailing, children }: Props = $props();
</script>

<button
  {onclick}
  class="w-full flex items-center gap-2.5 px-3 py-2.5 text-sm text-left transition-colors border-b border-gray-50 dark:border-lerd-border/50 {active
    ? 'bg-lerd-red/10 text-lerd-red'
    : 'text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-white/[0.03]'}"
>
  {#if leading}{@render leading()}{/if}
  <span class="font-medium flex-1 truncate">{@render children()}</span>
  {#if trailing}{@render trailing()}{/if}
</button>
