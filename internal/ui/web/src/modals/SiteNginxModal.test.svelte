<script lang="ts">
  import SiteNginxModal from './SiteNginxModal.svelte';
  import type { Site } from '$stores/sites';

  interface Props {
    open: boolean;
    site: Site;
    domain: string;
    onclose: () => void;
  }
  let { open, site, domain, onclose }: Props = $props();
</script>

<SiteNginxModal {site} {domain} {open} {onclose} />
