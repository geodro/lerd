<script lang="ts">
  import { onMount } from 'svelte';
  import type { Service } from '$stores/services';
  import { loadServices } from '$stores/services';
  import { suggestionFor, dismissSuggestion } from '$stores/presetSuggestions';
  import { loadPresets, installPreset, phaseLabel as defaultPhaseLabel, type Preset } from '$stores/presets';
  import { goToTab } from '$stores/route';
  import { m } from '../../paraglide/messages.js';

  function phaseLabel(p: Preset): string {
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

  interface Props {
    svc: Service;
  }
  let { svc }: Props = $props();

  const suggestion = $derived(suggestionFor(svc));

  onMount(() => {
    loadPresets();
  });

  async function install() {
    const p = $suggestion;
    if (!p) return;
    const r = await installPreset(p);
    if (r.ok && r.name) {
      await loadServices();
      await loadPresets();
      goToTab('services', r.name);
    }
  }

  function dismiss() {
    const p = $suggestion;
    if (p) dismissSuggestion(p.name);
  }
</script>

{#if $suggestion}
  <div class="mx-3 sm:mx-5 mt-4 rounded-lg border border-sky-200 dark:border-sky-500/30 bg-sky-50 dark:bg-sky-500/10 px-3 py-2.5">
    <div class="flex items-center gap-3">
      <svg class="w-4 h-4 shrink-0 text-sky-600 dark:text-sky-400" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/>
      </svg>
      <div class="flex-1 min-w-0">
        <p class="text-xs font-semibold text-sky-900 dark:text-sky-200">{m.services_banner_addPreset({ name: $suggestion.name })}</p>
        <p class="text-[11px] text-sky-700 dark:text-sky-300/80 mt-0.5">
          {m.services_banner_description({ description: $suggestion.description ?? '', svcName: svc.name })}
        </p>
        {#if $suggestion.error}
          <p class="text-[11px] text-red-500 mt-1">{$suggestion.error}</p>
        {/if}
      </div>
      <button
        onclick={install}
        disabled={Boolean($suggestion.installing)}
        title={$suggestion.installingMessage || ''}
        class="shrink-0 inline-flex items-center gap-1.5 text-xs font-medium bg-sky-600 hover:bg-sky-700 text-white rounded px-3 py-1.5 transition-colors disabled:opacity-50"
      >
        {#if $suggestion.installing}
          <svg class="w-3 h-3 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"/>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"/>
          </svg>
        {/if}
        {phaseLabel($suggestion)}
      </button>
      <button
        onclick={dismiss}
        title={m.services_banner_dismiss()}
        class="shrink-0 text-sky-600/60 hover:text-sky-700 dark:text-sky-400/60 dark:hover:text-sky-300 transition-colors"
      >
        <svg class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12"/>
        </svg>
      </button>
    </div>
  </div>
{/if}
