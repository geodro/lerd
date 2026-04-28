<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import NavRail from '$components/NavRail.svelte';
  import SidePanel from '$components/SidePanel.svelte';
  import MobileHeader from '$components/MobileHeader.svelte';
  import MobileNav from '$components/MobileNav.svelte';
  import MobileBackBar from '$components/MobileBackBar.svelte';
  import { tab, routeRest } from '$stores/route';
  import { loadVersion } from '$stores/version';
  import { loadAccessMode } from '$stores/accessMode';
  import { loadStatus } from '$stores/status';
  import { loadPhpVersions } from '$stores/phpVersions';
  import { loadNodeVersions } from '$stores/nodeVersions';
  import { loadAutostart } from '$stores/autostart';
  import { loadSites } from '$stores/sites';
  import { loadServices } from '$stores/services';
  import { connectWs, disconnectWs } from '$lib/ws';
  import { initDashboardRoute } from '$stores/dashboard';
  import { mobileView } from '$stores/mobileView';
  import ModalHost from './modals/ModalHost.svelte';
  import DashboardOverlay from '$components/DashboardOverlay.svelte';

  import SitesTab from '$tabs/SitesTab.svelte';
  import ServicesTab from '$tabs/ServicesTab.svelte';
  import SystemTab from '$tabs/SystemTab.svelte';
  import SitesDetail from '$tabs/SitesDetail.svelte';
  import ServicesDetail from '$tabs/ServicesDetail.svelte';
  import SystemDetail from '$tabs/SystemDetail.svelte';
  import AppsPage from '$tabs/AppsPage.svelte';

  onMount(() => {
    loadVersion();
    loadAccessMode();
    loadStatus();
    loadPhpVersions();
    loadNodeVersions();
    loadAutostart();
    loadSites();
    loadServices();
    connectWs();
    initDashboardRoute();
  });

  onDestroy(() => {
    disconnectWs();
  });

  // On mobile, show the detail pane once an item is selected (routeRest non-empty).
  // System tab always has a default selection (lerd) so we only show detail there
  // if the user explicitly picked something, to avoid jumping past the list.
  const showMobileDetail = $derived(Boolean($routeRest));
  const onApps = $derived($mobileView === 'apps');
</script>

<div class="h-screen flex">
  <NavRail />

  <SidePanel>
    {#if $tab === 'sites'}
      <SitesTab />
    {:else if $tab === 'services'}
      <ServicesTab />
    {:else if $tab === 'system'}
      <SystemTab />
    {/if}
  </SidePanel>

  <main class="flex-1 flex flex-col overflow-hidden">
    {#if !showMobileDetail}
      <MobileHeader />
    {/if}

    <div class="hidden md:flex flex-col flex-1 overflow-hidden">
      {#if $tab === 'sites'}
        <SitesDetail />
      {:else if $tab === 'services'}
        <ServicesDetail />
      {:else if $tab === 'system'}
        <SystemDetail />
      {/if}
    </div>

    {#if onApps}
      <div class="md:hidden flex-1 flex flex-col overflow-hidden pb-16">
        <AppsPage />
      </div>
    {:else if !showMobileDetail}
      <div class="md:hidden flex-1 overflow-y-auto pb-16">
        {#if $tab === 'sites'}
          <SitesTab />
        {:else if $tab === 'services'}
          <ServicesTab />
        {:else if $tab === 'system'}
          <SystemTab />
        {/if}
      </div>
    {:else}
      <div class="md:hidden flex-1 flex flex-col overflow-hidden pb-16">
        <MobileBackBar />
        {#if $tab === 'sites'}
          <SitesDetail />
        {:else if $tab === 'services'}
          <ServicesDetail />
        {:else if $tab === 'system'}
          <SystemDetail />
        {/if}
      </div>
    {/if}
  </main>

  <MobileNav />
  <ModalHost />
  <DashboardOverlay />
</div>
