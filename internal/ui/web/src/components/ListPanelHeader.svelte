<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    title: string;
    actions?: Snippet;
  }
  let { title, actions }: Props = $props();
</script>

<div
  class="flex items-center justify-between px-4 py-3 border-b border-gray-100 dark:border-lerd-border shrink-0 min-h-[48px]"
>
  <span class="text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider">
    {title}
  </span>
  {#if actions}
    <div class="flex items-center gap-1">
      {@render actions()}
    </div>
  {/if}
</div>
