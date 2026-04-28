<script lang="ts">
  import { tab, goToTab, TABS, type TabId } from '$stores/route';
  import IconButton from './IconButton.svelte';
  import Icon, { type IconName } from './Icon.svelte';
  import RailLogo from './RailLogo.svelte';
  import ThemeSwitcher from './ThemeSwitcher.svelte';
  import VersionLabel from './VersionLabel.svelte';
  import { dashboardServices, dashboardOpen, openDashboard, openDocs } from '$stores/dashboard';
  import { dashboardIconSvg } from '$lib/dashboardIcons';
  import { serviceLabel } from '$stores/services';
  import { m } from '../paraglide/messages.js';

  const labels = $derived<Record<TabId, string>>({
    sites: m.nav_sites(),
    services: m.nav_services(),
    system: m.nav_system()
  });

  const icons: Record<TabId, IconName> = {
    sites: 'sites',
    services: 'services',
    system: 'system'
  };
</script>

<aside
  class="hidden md:flex flex-col items-center w-14 shrink-0 bg-white dark:bg-lerd-card border-r border-gray-200 dark:border-lerd-border py-3 z-20"
>
  <RailLogo />

  <div class="flex flex-col gap-1">
    {#each TABS as t (t)}
      <IconButton
        title={labels[t]}
        active={!$dashboardOpen && $tab === t}
        onclick={() => goToTab(t)}
      >
        <Icon name={icons[t]} />
      </IconButton>
    {/each}
  </div>

  {#if $dashboardServices.length > 0}
    <div class="flex flex-col items-center gap-1 mt-3 pt-3 border-t border-gray-200 dark:border-lerd-border w-8">
      {#each $dashboardServices as svc (svc.name)}
        <IconButton
          title={serviceLabel(svc.name) + ' ' + m.services_dashboard().toLowerCase()}
          active={$dashboardOpen?.name === svc.name}
          onclick={() => openDashboard(svc)}
        >
          <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            {@html dashboardIconSvg(svc.name)}
          </svg>
        </IconButton>
      {/each}
    </div>
  {/if}

  <div class="mt-auto flex flex-col items-center gap-2">
    <IconButton
      title={m.nav_documentation()}
      active={$dashboardOpen?.name === 'docs'}
      onclick={openDocs}
    >
      <Icon name="docs" />
    </IconButton>
    <ThemeSwitcher size="sm" />
    <VersionLabel />
  </div>
</aside>
