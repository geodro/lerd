<script lang="ts">
  import { routeRest } from '$stores/route';
  import { sites } from '$stores/sites';
  import EmptyState from '$components/EmptyState.svelte';
  import SiteDetail from './sites/SiteDetail.svelte';
  import { m } from '../paraglide/messages.js';

  const selected = $derived($routeRest);
  const site = $derived($sites.find((s) => s.domain === selected));
</script>

{#if site}
  {#key site.domain}
    <SiteDetail {site} />
  {/key}
{:else}
  <div class="flex-1 flex items-center justify-center">
    <EmptyState title={m.sites_select()} />
  </div>
{/if}
