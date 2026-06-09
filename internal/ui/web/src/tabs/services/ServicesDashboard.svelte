<script lang="ts">
  import { onMount } from 'svelte';
  import StatusDot from '$components/StatusDot.svelte';
  import Icon from '$components/Icon.svelte';
  import DashboardHeader from '$components/DashboardHeader.svelte';
  import DashboardSection from '$components/DashboardSection.svelte';
  import InstalledServiceTile from './InstalledServiceTile.svelte';
  import PresetCard from './PresetCard.svelte';
  import { coreServices, servicesLoaded } from '$stores/services';
  import { discoverablePresets, presetsLoaded, loadPresets } from '$stores/presets';
  import { openPresetModal } from '$stores/modals';
  import { accessMode } from '$stores/accessMode';
  import { groupByCategory, CATEGORY_LABELS } from '$lib/presetCategories';
  import { m } from '../../paraglide/messages.js';

  onMount(() => {
    loadPresets();
  });

  const running = $derived($coreServices.filter((s) => s.status === 'active').length);
  const total = $derived($coreServices.length);
  const updates = $derived($coreServices.filter((s) => s.update_available).length);
  const sitesServed = $derived(
    new Set($coreServices.flatMap((s) => s.site_domains ?? [])).size
  );

  const groups = $derived(groupByCategory($discoverablePresets));
</script>

<div class="flex-1 overflow-y-auto">
  <DashboardHeader title={m.services_dash_overview()} stats={$servicesLoaded && total > 0 ? summary : undefined} />

  <div class="p-4 space-y-6">
    <section class="space-y-2.5">
      <h2 class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-gray-500">{m.services_dash_installed()}</h2>
      {#if $servicesLoaded && total === 0}
        <p class="text-sm text-gray-500 dark:text-gray-400">{m.services_dash_noInstalled()}</p>
      {:else}
        <div class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-2">
          {#each $coreServices as svc (svc.name)}
            <InstalledServiceTile {svc} />
          {/each}
        </div>
      {/if}
    </section>

    {#if $accessMode.loopback}
      <section class="space-y-3">
        <div class="flex items-center justify-between gap-3">
          <h2 class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-gray-500">{m.services_dash_discover()}</h2>
          <button
            onclick={openPresetModal}
            class="inline-flex items-center gap-1 text-xs font-medium text-lerd-red hover:text-lerd-redhov"
          >
            <Icon name="plus" class="w-3.5 h-3.5" />
            {m.services_addPreset()}
          </button>
        </div>
        {#if !$presetsLoaded}
          <p class="text-xs text-gray-400">{m.common_loading()}</p>
        {:else if groups.length === 0}
          <p class="text-sm text-gray-500 dark:text-gray-400">{m.services_preset_allInstalled()}</p>
        {:else}
          <div class="space-y-4">
            {#each groups as group (group.key)}
              <DashboardSection label={CATEGORY_LABELS[group.key]()} sub>
                {#each group.presets as preset (preset.name)}
                  <PresetCard {preset} category={group.key} />
                {/each}
              </DashboardSection>
            {/each}
          </div>
        {/if}
      </section>
    {/if}
  </div>
</div>

{#snippet summary()}
  <span class="inline-flex items-center gap-1.5">
    <StatusDot color={running > 0 ? 'green' : 'gray'} />
    {m.dashboard_services_summary({ running, total })}
  </span>
  {#if updates > 0}
    <span class="inline-flex items-center gap-1.5 text-emerald-600 dark:text-emerald-400">
      <span>↑</span>
      {m.dashboard_services_updates({ count: updates })}
    </span>
  {/if}
  {#if sitesServed > 0}
    <span class="inline-flex items-center gap-1.5">
      <Icon name="sites" class="w-3.5 h-3.5" />
      {m.services_dash_sitesServed({ count: sitesServed })}
    </span>
  {/if}
{/snippet}
