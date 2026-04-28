<script lang="ts">
  import EmptyState from './EmptyState.svelte';

  interface Props {
    title: string;
    withHint?: boolean;
    size?: 'sm' | 'md';
  }
  let { title, withHint = false, size }: Props = $props();
</script>

{#if withHint}
  {#snippet hint()}Some hint text{/snippet}
  <EmptyState {title} {size} {hint} />
{:else}
  <EmptyState {title} {size} />
{/if}
