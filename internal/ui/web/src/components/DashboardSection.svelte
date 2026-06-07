<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    label: string;
    sub?: boolean;
    children: Snippet;
  }
  let { label, sub = false, children }: Props = $props();
</script>

<section class={sub ? 'space-y-2' : 'space-y-2.5'}>
  {#if sub}
    <h3 class="text-[11px] font-semibold uppercase tracking-wider text-gray-400 dark:text-gray-500">{label}</h3>
  {:else}
    <h2 class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-gray-500">{label}</h2>
  {/if}
  <div class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-2">
    {@render children()}
  </div>
</section>
