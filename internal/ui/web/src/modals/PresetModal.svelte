<script lang="ts">
  import { onMount } from 'svelte';
  import Modal from '$components/Modal.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import { closeModal } from '$stores/modals';
  import {
    presets,
    presetsLoaded,
    installablePresets,
    availableVersions,
    installPreset,
    loadPresets,
    type Preset
  } from '$stores/presets';
  import { loadServices } from '$stores/services';
  import { goToTab } from '$stores/route';
  import { m } from '../paraglide/messages.js';

  onMount(() => {
    loadPresets();
  });

  function localPhaseLabel(p: Preset): string {
    if (!p.installing) return m.services_preset_phase_add();
    switch (p.installingPhase) {
      case 'installing_config':
        return m.services_preset_phase_installingConfig();
      case 'starting_deps':
        return p.installingDep
          ? m.services_preset_phase_startingDep({ dep: p.installingDep })
          : m.services_preset_phase_startingDeps();
      case 'pulling_image':
        return m.services_preset_phase_pullingImage();
      case 'starting_unit':
        return m.services_preset_phase_startingUnit();
      case 'waiting_ready':
        return m.services_preset_phase_waitingReady();
      default:
        return m.services_preset_phase_adding();
    }
  }

  function setSelectedVersion(name: string, tag: string) {
    presets.update((list) => list.map((p) => (p.name === name ? { ...p, selected_version: tag } : p)));
  }

  async function onInstall(p: Preset) {
    const r = await installPreset(p);
    if (r.ok && r.name) {
      await loadServices();
      closeModal();
      goToTab('services', r.name);
    }
    loadPresets();
  }
</script>

<Modal open title={m.services_preset_title()} onclose={closeModal}>
  <div class="px-5 py-3 border-b border-gray-100 dark:border-lerd-border">
    <p class="text-xs text-gray-500 dark:text-gray-400">{m.services_preset_subtitle()}</p>
  </div>
  <div class="px-5 py-3 max-h-80 overflow-y-auto">
    {#if !$presetsLoaded}
      <div class="py-4 text-center text-xs text-gray-400">{m.common_loading()}</div>
    {:else if $installablePresets.length === 0}
      <div class="py-4 text-center text-xs text-gray-400">{m.services_preset_allInstalled()}</div>
    {:else}
      {#each $installablePresets as p (p.name)}
        <div class="border border-gray-100 dark:border-lerd-border rounded-lg p-3 mb-2 last:mb-0">
          <div class="flex items-center gap-3">
            <div class="flex-1 min-w-0">
              <div class="flex items-center gap-2">
                <span class="text-sm font-semibold text-gray-900 dark:text-white">{p.name}</span>
                {#if (p.installed_tags || []).length > 0}
                  <span class="text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded bg-emerald-50 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-400">
                    {m.services_preset_installedTag({ tags: (p.installed_tags || []).join(', ') })}
                  </span>
                {/if}
              </div>
              {#if p.description}
                <p class="text-xs text-gray-500 dark:text-gray-400 mt-0.5">{p.description}</p>
              {/if}
              {#if p.image}
                <div class="text-[11px] text-gray-400 dark:text-gray-500 mt-1 font-mono truncate">{p.image}</div>
              {/if}
              {#if p.depends_on && p.depends_on.length > 0}
                <div class="text-[11px] text-gray-400 dark:text-gray-500 mt-1">{m.services_preset_dependsOn()} {p.depends_on.join(', ')}</div>
              {/if}
              {#if p.dashboard}
                <div class="text-[11px] text-gray-400 dark:text-gray-500 mt-0.5">{m.services_preset_dashboard()} {p.dashboard}</div>
              {/if}
              {#if (p.missing_deps || []).length > 0}
                <div class="text-[11px] text-amber-600 dark:text-amber-400 mt-1">{m.services_preset_installFirst({ deps: (p.missing_deps || []).join(', ') })}</div>
              {/if}
            </div>
            {#if (p.versions || []).length > 0}
              <select
                value={p.selected_version ?? ''}
                onchange={(e) => setSelectedVersion(p.name, (e.target as HTMLSelectElement).value)}
                class="shrink-0 text-xs bg-gray-50 dark:bg-lerd-bg border border-gray-200 dark:border-lerd-border rounded px-2 py-1.5 text-gray-700 dark:text-gray-300 focus:outline-none focus:border-lerd-red/50"
              >
                {#each availableVersions(p) as v (v.tag)}
                  <option value={v.tag}>{v.label || v.tag}</option>
                {/each}
              </select>
            {/if}
            <DetailButton
              tone="primary"
              onclick={() => onInstall(p)}
              disabled={Boolean(p.installing) || (p.missing_deps || []).length > 0}
              loading={Boolean(p.installing)}
            >
              {localPhaseLabel(p)}
            </DetailButton>
          </div>
          {#if p.installing && p.installingMessage}
            <p class="text-[11px] text-gray-400 dark:text-gray-500 mt-2 font-mono truncate">{p.installingMessage}</p>
          {/if}
          {#if p.error}
            <p class="text-[11px] text-red-500 mt-2">{p.error}</p>
          {/if}
        </div>
      {/each}
    {/if}
  </div>

  {#snippet footer()}
    <DetailButton onclick={closeModal}>{m.common_close()}</DetailButton>
  {/snippet}
</Modal>
