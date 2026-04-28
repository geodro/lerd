<script lang="ts">
  import ListPanel from '$components/ListPanel.svelte';
  import ActionButton from '$components/ActionButton.svelte';
  import EmptyState from '$components/EmptyState.svelte';
  import Icon from '$components/Icon.svelte';
  import ListRow from '$components/ListRow.svelte';
  import StatusDot from '$components/StatusDot.svelte';
  import LoadingRow from '$components/LoadingRow.svelte';
  import { accessMode } from '$stores/accessMode';
  import { routeRest, goToTab } from '$stores/route';
  import {
    coreServices,
    workerGroups,
    servicesLoaded,
    serviceLabel,
    workerSiteName
  } from '$stores/services';
  import { openPresetModal } from '$stores/modals';
  import { m } from '../paraglide/messages.js';

  const selected = $derived($routeRest);

  function select(name: string) {
    goToTab('services', name);
  }

  function groupLabel(key: string): string {
    if (key === 'queue') return m.services_groups_queues();
    if (key === 'horizon') return m.services_groups_horizon();
    if (key === 'schedule') return m.services_groups_schedules();
    if (key === 'reverb') return m.services_groups_reverb();
    if (key === 'stripe') return m.services_groups_stripe();
    if (key === 'workers') return m.services_groups_workers();
    return key;
  }
</script>

{#snippet actions()}
  {#if $accessMode.loopback}
    <ActionButton title={m.services_addPreset()} tone="accent" onclick={openPresetModal}>
      <Icon name="plus" class="w-3.5 h-3.5" />
    </ActionButton>
  {/if}
{/snippet}

<ListPanel title={m.services_title()} {actions}>
  {#if !$servicesLoaded}
    <LoadingRow />
  {:else if $coreServices.length === 0 && $workerGroups.length === 0}
    <EmptyState title={m.services_empty()} size="sm" />
  {:else}
    {#each $coreServices as svc (svc.name)}
      {#snippet leading()}<StatusDot color={svc.status === 'active' ? 'green' : 'gray'} />{/snippet}
      {#snippet trailing()}
        {#if svc.depends_on && svc.depends_on.length > 0}
          <span class="shrink-0 text-gray-300 dark:text-gray-600" title={m.services_hasDependencies()}>
            <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <circle cx="18" cy="5" r="3"/><circle cx="6" cy="12" r="3"/><circle cx="18" cy="19" r="3"/>
              <path d="M8.59 13.51l6.83 3.98M15.41 6.51l-6.82 3.98"/>
            </svg>
          </span>
        {/if}
        {#if svc.site_count > 0}
          <span class="text-[10px] font-medium tabular-nums shrink-0 {selected === svc.name ? 'text-lerd-red/70' : 'text-gray-400 dark:text-gray-600'}">{svc.site_count}</span>
        {/if}
      {/snippet}
      <ListRow active={selected === svc.name} onclick={() => select(svc.name)} {leading} {trailing}>
        {serviceLabel(svc.name)}
        {#if svc.version}
          <span class="ml-1 text-[10px] font-normal tabular-nums text-gray-400 dark:text-gray-500">{svc.version}</span>
        {/if}
        {#if svc.update_available}
          <span
            class="ml-1 text-[10px] font-medium text-emerald-600 dark:text-emerald-400"
            title={svc.latest_version ? m.services_updateAvailableTo({ tag: svc.latest_version }) : m.services_updateAvailable()}
          >↑</span>
        {/if}
      </ListRow>
    {/each}

    {#each $workerGroups as group (group.key)}
      <div class="border-t border-gray-100 dark:border-lerd-border">
        <div class="px-3 py-1.5 text-[10px] font-semibold uppercase tracking-wider text-gray-400 dark:text-gray-500">{groupLabel(group.key)}</div>
        {#each group.items as svc (svc.name)}
          {#snippet workerLeading()}<StatusDot color={svc.status === 'active' ? 'green' : 'gray'} size="xs" />{/snippet}
          <ListRow active={selected === svc.name} onclick={() => select(svc.name)} leading={workerLeading}>
            {workerSiteName(svc)}
          </ListRow>
        {/each}
      </div>
    {/each}
  {/if}
</ListPanel>
