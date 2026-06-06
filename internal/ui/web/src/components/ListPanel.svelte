<script lang="ts">
  import type { Snippet } from 'svelte';
  import ListPanelHeader from './ListPanelHeader.svelte';

  interface Props {
    title: string;
    actions?: Snippet;
    overlay?: Snippet; // pinned to the panel, floats over the scroll area
    children: Snippet;
  }
  let { title, actions, overlay, children }: Props = $props();
</script>

<div class="relative flex flex-col h-full">
  <ListPanelHeader {title} {actions} />
  <div class="flex-1 overflow-y-auto">
    {@render children()}
  </div>
  {#if overlay}{@render overlay()}{/if}
</div>
