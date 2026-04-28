<script lang="ts">
  import DetailPanel from '$components/DetailPanel.svelte';
  import SiteHeader from './SiteHeader.svelte';
  import SiteControls from './SiteControls.svelte';
  import SiteLogs from './SiteLogs.svelte';
  import WorktreeList from './WorktreeList.svelte';
  import type { Site } from '$stores/sites';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();
</script>

<DetailPanel>
  <SiteHeader {site} />
  {#if !site.paused}
    <SiteControls {site} />
  {/if}
  <WorktreeList {site} />
  <SiteLogs {site} />
</DetailPanel>
