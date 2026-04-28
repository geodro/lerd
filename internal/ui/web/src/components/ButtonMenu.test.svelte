<script lang="ts">
  import ButtonMenu, { type ButtonMenuAction } from './ButtonMenu.svelte';

  interface Props {
    actions: ButtonMenuAction[];
    busy?: boolean;
  }
  let { actions, busy = false }: Props = $props();
</script>

<ButtonMenu {actions} {busy} />
