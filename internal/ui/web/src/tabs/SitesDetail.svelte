<script lang="ts">
  import { routeRest } from '$stores/route';
  import { sites } from '$stores/sites';
  import SiteDetail from './sites/SiteDetail.svelte';
  import SitesDashboard from './sites/SitesDashboard.svelte';

  // routeRest may be "<domain>" or "<domain>/<subtab>" (e.g. dump notification
  // deep-links into the per-site dumps view). The sub-tab is handled inside
  // SiteDetail; here we just match the first segment as the site domain.
  const parts = $derived($routeRest.split('/'));
  const selected = $derived(parts[0] ?? '');
  const site = $derived($sites.find((s) => s.domain === selected));
</script>

{#if site}
  {#key site.domain}
    <SiteDetail {site} />
  {/key}
{:else}
  <SitesDashboard />
{/if}
