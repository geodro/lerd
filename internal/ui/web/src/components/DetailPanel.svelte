<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    children: Snippet;
  }
  let { children }: Props = $props();
</script>

<div class="flex-1 flex flex-col overflow-hidden min-h-0">
  {@render children()}
</div>
