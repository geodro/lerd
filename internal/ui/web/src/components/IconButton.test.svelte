<script lang="ts">
  import IconButton from './IconButton.svelte';

  interface Props {
    title: string;
    active: boolean;
    onclick?: (e: MouseEvent) => void;
  }
  let { title, active, onclick }: Props = $props();
</script>

<IconButton {title} {active} {onclick}>X</IconButton>
