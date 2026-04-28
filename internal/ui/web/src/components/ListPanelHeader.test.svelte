<script lang="ts">
  import ListPanelHeader from './ListPanelHeader.svelte';

  interface Props {
    title: string;
    withActions?: boolean;
  }
  let { title, withActions = false }: Props = $props();
</script>

{#if withActions}
  {#snippet actions()}<button title="act">A</button>{/snippet}
  <ListPanelHeader {title} {actions} />
{:else}
  <ListPanelHeader {title} />
{/if}
