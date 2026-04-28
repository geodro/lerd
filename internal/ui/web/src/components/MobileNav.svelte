<script lang="ts">
  import { tab, goToTab, TABS, type TabId } from '$stores/route';
  import Icon, { type IconName } from './Icon.svelte';
  import { dashboardOpen } from '$stores/dashboard';
  import { mobileView, goToApps } from '$stores/mobileView';
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

  const onTabView = $derived($mobileView === 'tab' && !$dashboardOpen);
</script>

<nav
  class="md:hidden fixed bottom-0 left-0 right-0 z-30 flex h-16 border-t border-gray-200 dark:border-lerd-border bg-white dark:bg-lerd-card"
>
  {#each TABS as t (t)}
    <button
      onclick={() => goToTab(t)}
      class="grow basis-0 min-w-0 flex flex-col items-center justify-center gap-0.5 transition-colors {onTabView && $tab === t
        ? 'text-lerd-red'
        : 'text-gray-400 dark:text-gray-500'}"
    >
      <Icon name={icons[t]} class="w-5 h-5" />
      <span class="text-[10px] font-medium">{labels[t]}</span>
    </button>
  {/each}
  <button
    onclick={goToApps}
    class="grow basis-0 min-w-0 flex flex-col items-center justify-center gap-0.5 transition-colors {$mobileView === 'apps'
      ? 'text-lerd-red'
      : 'text-gray-400 dark:text-gray-500'}"
  >
    <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M4 6a2 2 0 012-2h4a2 2 0 012 2v4a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h4a2 2 0 012 2v4a2 2 0 01-2 2H6a2 2 0 01-2-2v-4zM14 6a2 2 0 012-2h4a2 2 0 012 2v4a2 2 0 01-2 2h-4a2 2 0 01-2-2V6zM14 16a2 2 0 012-2h4a2 2 0 012 2v4a2 2 0 01-2 2h-4a2 2 0 01-2-2v-4z"/>
    </svg>
    <span class="text-[10px] font-medium">{m.nav_apps()}</span>
  </button>
</nav>
