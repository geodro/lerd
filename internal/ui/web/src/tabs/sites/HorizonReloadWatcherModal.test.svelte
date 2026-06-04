<script lang="ts">
  import HorizonReloadWatcherModal from './HorizonReloadWatcherModal.svelte';
  import type { Site } from '$stores/sites';

  interface Props {
    open?: boolean;
    site: Site;
    oninstalled?: () => void;
    onclose?: () => void;
  }
  let { open = true, site, oninstalled = () => {}, onclose = () => {} }: Props = $props();
</script>

<HorizonReloadWatcherModal {open} {site} {oninstalled} {onclose} />
