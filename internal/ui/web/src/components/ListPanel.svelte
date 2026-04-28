<script lang="ts">
  import type { Snippet } from 'svelte';
  import ListPanelHeader from './ListPanelHeader.svelte';

  interface Props {
    title: string;
    actions?: Snippet;
    children: Snippet;
  }
  let { title, actions, children }: Props = $props();
</script>

<div class="flex flex-col h-full">
  <ListPanelHeader {title} {actions} />
  <div class="flex-1 overflow-y-auto">
    {@render children()}
  </div>
</div>
