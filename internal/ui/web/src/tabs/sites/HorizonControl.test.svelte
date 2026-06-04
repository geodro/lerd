<script lang="ts">
  import HorizonControl from './HorizonControl.svelte';

  interface Props {
    running?: boolean;
    failing?: boolean;
    reload?: boolean;
    horizonLoading?: boolean;
    reloadLoading?: boolean;
    onToggle?: () => void;
    onToggleReload?: () => void;
  }
  let {
    running = false,
    failing = false,
    reload = false,
    horizonLoading = false,
    reloadLoading = false,
    onToggle = () => {},
    onToggleReload = () => {}
  }: Props = $props();
</script>

<HorizonControl
  {running}
  {failing}
  {reload}
  {horizonLoading}
  {reloadLoading}
  {onToggle}
  {onToggleReload}
/>
