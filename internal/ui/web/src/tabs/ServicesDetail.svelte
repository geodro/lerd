<script lang="ts">
  import { routeRest } from '$stores/route';
  import { services } from '$stores/services';
  import EmptyState from '$components/EmptyState.svelte';
  import ServiceDetail from './services/ServiceDetail.svelte';
  import { m } from '../paraglide/messages.js';

  const selected = $derived($routeRest);
  const svc = $derived($services.find((s) => s.name === selected));
</script>

{#if svc}
  <ServiceDetail {svc} />
{:else}
  <div class="flex-1 flex items-center justify-center">
    <EmptyState title={m.services_select()} />
  </div>
{/if}
