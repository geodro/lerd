<script lang="ts">
  import ListRow from './ListRow.svelte';

  interface Props {
    active?: boolean;
    withLeading?: boolean;
    withTrailing?: boolean;
    onclick?: (e: MouseEvent) => void;
    label: string;
  }
  let { active, withLeading, withTrailing, onclick, label }: Props = $props();
</script>

{#snippet leading()}<span data-testid="lead">L</span>{/snippet}
{#snippet trailing()}<span data-testid="tail">T</span>{/snippet}

<ListRow
  {active}
  {onclick}
  leading={withLeading ? leading : undefined}
  trailing={withTrailing ? trailing : undefined}
>{label}</ListRow>
