<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { isEditableTarget } from '$lib/dom';
  import {
    sites,
    openSiteInBrowser,
    toggleTLS,
    toggleLANShare,
    toggleQueue,
    toggleHorizon,
    toggleSchedule,
    toggleReverb,
    toggleStripe,
    toggleWorker,
    loadSites
  } from '$stores/sites';
  import { coreServices, serviceLabel } from '$stores/services';
  import { unhealthyWorkers, healAll, loadWorkerHealth } from '$stores/workerHealth';
  import { openLinkModal, openPresetModal } from '$stores/modals';
  import { openDocs } from '$stores/dashboard';
  import { theme } from '$stores/theme';
  import { loadVersion } from '$stores/version';
  import { goToTab } from '$stores/route';
  import { accessMode } from '$stores/accessMode';
  import { paletteOpen, openCommandPalette, closeCommandPalette } from '$stores/commandPalette';
  import {
    commandsBySiteStore,
    preloadCommandsFor,
    launchCommand,
    type Command
  } from '$stores/commands';
  import { m } from '../paraglide/messages.js';

  type Group = 'pages' | 'sites' | 'services' | 'toggles' | 'commands' | 'actions';
  interface Entry {
    id: string;
    label: string;
    hint?: string;
    group: Group;
    action: () => void | Promise<void>;
  }

  let query = $state('');
  let selected = $state(0);
  let inputEl: HTMLInputElement | null = $state(null);
  let listEl: HTMLUListElement | null = $state(null);

  const entries: Entry[] = $derived.by(() => {
    const list: Entry[] = [];

    list.push({ id: 'page:dashboard', label: m.nav_dashboard(), group: 'pages', action: () => goToTab('dashboard') });
    list.push({ id: 'page:sites', label: m.nav_sites(), group: 'pages', action: () => goToTab('sites') });
    list.push({ id: 'page:services', label: m.nav_services(), group: 'pages', action: () => goToTab('services') });
    list.push({ id: 'page:system', label: m.nav_system(), group: 'pages', action: () => goToTab('system') });

    for (const s of $sites) {
      list.push({
        id: 'site:' + s.domain,
        label: s.domain,
        hint: s.framework_label || s.framework,
        group: 'sites',
        action: () => goToTab('sites', s.domain)
      });
    }

    for (const svc of $coreServices) {
      list.push({
        id: 'svc:' + svc.name,
        label: serviceLabel(svc.name),
        hint: svc.version || (svc.status === 'active' ? 'active' : 'inactive'),
        group: 'services',
        action: () => goToTab('services', svc.name)
      });
    }

    // Toggles: site-level state toggles (HTTPS, LAN share) and worker
    // start/stop entries. Labels flip between Enable/Disable or Start/Stop
    // based on current state, so searches like "stop queue" only surface
    // running queues and "enable https" only insecure sites.
    for (const s of $sites) {
      const d = s.domain;
      const refresh = async () => { await loadSites(); };

      // HTTPS toggle
      {
        const on = Boolean(s.tls);
        list.push({
          id: 'tgl:' + d + ':tls',
          label: m.palette_toggle({ action: on ? m.common_disable() : m.common_enable(), feature: 'HTTPS' }),
          hint: d,
          group: 'toggles',
          action: async () => { await toggleTLS(s); await refresh(); }
        });
      }

      // LAN share toggle
      {
        const on = Boolean(s.lan_port);
        list.push({
          id: 'tgl:' + d + ':lan',
          label: m.palette_toggle({ action: on ? m.common_stop() : m.common_start(), feature: m.palette_featLanShare() }),
          hint: d,
          group: 'toggles',
          action: async () => { await toggleLANShare(s, ''); await refresh(); }
        });
      }

      if (s.has_queue_worker) {
        const on = Boolean(s.queue_running);
        list.push({
          id: 'tgl:' + d + ':queue',
          label: m.palette_toggle({ action: on ? m.common_stop() : m.common_start(), feature: m.palette_featQueueWorker() }),
          hint: d,
          group: 'toggles',
          action: async () => { await toggleQueue(s); await refresh(); }
        });
      }
      if (s.has_horizon) {
        const on = Boolean(s.horizon_running);
        list.push({
          id: 'tgl:' + d + ':horizon',
          label: m.palette_toggle({ action: on ? m.common_stop() : m.common_start(), feature: 'Horizon' }),
          hint: d,
          group: 'toggles',
          action: async () => { await toggleHorizon(s); await refresh(); }
        });
      }
      if (s.has_schedule_worker) {
        const on = Boolean(s.schedule_running);
        list.push({
          id: 'tgl:' + d + ':schedule',
          label: m.palette_toggle({ action: on ? m.common_stop() : m.common_start(), feature: m.palette_featScheduler() }),
          hint: d,
          group: 'toggles',
          action: async () => { await toggleSchedule(s); await refresh(); }
        });
      }
      if (s.has_reverb) {
        const on = Boolean(s.reverb_running);
        list.push({
          id: 'tgl:' + d + ':reverb',
          label: m.palette_toggle({ action: on ? m.common_stop() : m.common_start(), feature: 'Reverb' }),
          hint: d,
          group: 'toggles',
          action: async () => { await toggleReverb(s); await refresh(); }
        });
      }
      if (s.stripe_secret_set) {
        const on = Boolean(s.stripe_running);
        list.push({
          id: 'tgl:' + d + ':stripe',
          label: m.palette_toggle({ action: on ? m.common_stop() : m.common_start(), feature: 'Stripe listener' }),
          hint: d,
          group: 'toggles',
          action: async () => { await toggleStripe(s); await refresh(); }
        });
      }
      for (const w of s.framework_workers || []) {
        const on = Boolean(w.running);
        const lbl = w.label || w.name;
        list.push({
          id: 'tgl:' + d + ':fw:' + w.name,
          label: m.palette_toggle({ action: on ? m.common_stop() : m.common_start(), feature: lbl }),
          hint: d,
          group: 'toggles',
          action: async () => { await toggleWorker(s, w); await refresh(); }
        });
      }
    }

    // Commands: per (site, command) entry from the cached lists. Cache
    // populates on palette open; entries appear once the fetch lands.
    for (const s of $sites) {
      const list2 = $commandsBySiteStore[s.domain] as Command[] | undefined;
      if (!list2) continue;
      for (const c of list2) {
        list.push({
          id: 'cmd:' + s.domain + ':' + c.name,
          label: m.palette_run({ label: c.label || c.name }),
          hint: s.domain + ' · ' + c.name,
          group: 'commands',
          action: () => launchCommand(s.domain, c)
        });
      }
    }

    if ($accessMode.loopback) {
      list.push({ id: 'act:link', label: m.palette_action_link(), group: 'actions', action: openLinkModal });
      list.push({ id: 'act:preset', label: m.palette_action_addService(), group: 'actions', action: openPresetModal });
    }
    if ($unhealthyWorkers.length > 0) {
      list.push({
        id: 'act:heal',
        label: m.palette_action_heal({ count: $unhealthyWorkers.length }),
        group: 'actions',
        action: async () => {
          await healAll();
          await loadWorkerHealth();
        }
      });
    }
    list.push({ id: 'act:checkUpdates', label: m.palette_action_checkUpdates(), group: 'actions', action: () => loadVersion() });
    list.push({ id: 'act:docs', label: m.palette_action_openDocs(), group: 'actions', action: openDocs });
    list.push({
      id: 'act:openInBrowser',
      label: m.palette_action_openCurrentSite(),
      hint: 'browser',
      group: 'actions',
      action: () => {
        const cur = $sites.find((s) => location.hash.startsWith('#sites/' + s.domain));
        if (cur) openSiteInBrowser(cur);
      }
    });
    list.push({
      id: 'act:theme',
      label: m.palette_action_toggleTheme(),
      group: 'actions',
      action: () => theme.update((t) => (t === 'dark' ? 'light' : 'dark'))
    });

    return list;
  });

  const filtered = $derived.by(() => {
    const terms = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
    if (terms.length === 0) return entries;
    return entries.filter((e) => {
      const hay = (e.label + ' ' + (e.hint || '')).toLowerCase();
      return terms.every((t) => hay.includes(t));
    });
  });

  const groupLabel: Record<Group, () => string> = {
    pages: () => m.palette_group_pages(),
    sites: () => m.palette_group_sites(),
    services: () => m.palette_group_services(),
    toggles: () => m.palette_groupToggles(),
    commands: () => m.cmd_commands(),
    actions: () => m.palette_group_actions()
  };

  function openPalette() {
    query = '';
    selected = 0;
    openCommandPalette();
    queueMicrotask(() => inputEl?.focus());
    void preloadCommandsFor($sites.map((s) => s.domain));
  }

  function closePalette() {
    closeCommandPalette();
  }

  async function execute(e: Entry) {
    closePalette();
    await Promise.resolve(e.action());
  }

  // External openers (e.g. dashboard header button) flip the store directly;
  // re-focus the input and reset the query whenever it transitions to open.
  $effect(() => {
    if ($paletteOpen) {
      query = '';
      selected = 0;
      queueMicrotask(() => inputEl?.focus());
    }
  });

  const isInInput = isEditableTarget;

  function onKeydown(e: KeyboardEvent) {
    const isCmdK = (e.metaKey || e.ctrlKey) && (e.key === 'k' || e.key === 'K');
    if (isCmdK) {
      e.preventDefault();
      $paletteOpen ? closePalette() : openPalette();
      return;
    }
    if (!$paletteOpen && e.key === '/' && !isInInput(e.target)) {
      e.preventDefault();
      openPalette();
      return;
    }
    if (!$paletteOpen) return;
    if (e.key === 'Escape') {
      e.preventDefault();
      closePalette();
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      selected = Math.min(filtered.length - 1, selected + 1);
      scrollSelectedIntoView();
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      selected = Math.max(0, selected - 1);
      scrollSelectedIntoView();
    } else if (e.key === 'Enter') {
      e.preventDefault();
      const entry = filtered[selected];
      if (entry) execute(entry);
    }
  }

  function scrollSelectedIntoView() {
    queueMicrotask(() => {
      const node = listEl?.querySelector<HTMLElement>('[data-selected="true"]');
      node?.scrollIntoView({ block: 'nearest' });
    });
  }

  $effect(() => {
    void query;
    selected = 0;
  });

  onMount(() => window.addEventListener('keydown', onKeydown));
  onDestroy(() => window.removeEventListener('keydown', onKeydown));
</script>

{#if $paletteOpen}
  <div
    class="fixed inset-0 z-80 bg-black/50 backdrop-blur-xs flex items-start justify-center pt-[15vh] px-4"
    onclick={(e) => { if (e.target === e.currentTarget) closePalette(); }}
    role="presentation"
  >
    <div
      class="w-full max-w-xl max-h-[70vh] flex flex-col bg-white dark:bg-lerd-card border border-gray-200 dark:border-lerd-border rounded-xl shadow-2xl overflow-hidden"
      role="dialog"
      aria-modal="true"
      aria-label={m.palette_ariaLabel()}
      tabindex="-1"
    >
      <div class="flex items-center gap-2 px-4 py-3 border-b border-gray-100 dark:border-lerd-border">
        <svg class="w-4 h-4 text-gray-400 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"/>
        </svg>
        <input
          bind:this={inputEl}
          bind:value={query}
          type="text"
          placeholder={m.palette_placeholder()}
          class="flex-1 bg-transparent text-sm text-gray-800 dark:text-gray-100 placeholder:text-gray-400 dark:placeholder:text-gray-500 focus:outline-hidden"
          autocomplete="off"
          spellcheck="false"
        />
        <kbd class="hidden sm:inline-flex items-center text-[10px] font-mono text-gray-400 dark:text-gray-500 border border-gray-200 dark:border-lerd-border rounded-sm px-1.5 py-0.5">esc</kbd>
      </div>

      <ul bind:this={listEl} class="flex-1 overflow-y-auto py-1">
        {#if filtered.length === 0}
          <li class="px-4 py-6 text-center text-sm text-gray-400 dark:text-gray-500">{m.palette_empty()}</li>
        {:else}
          {#each filtered as e, idx (e.id)}
            {#if idx === 0 || filtered[idx - 1].group !== e.group}
              <li class="px-4 pt-3 pb-1 text-[10px] font-semibold uppercase tracking-wider text-gray-400 dark:text-gray-500">{groupLabel[e.group]()}</li>
            {/if}
            {@const isActive = idx === selected}
            <li>
              <button
                type="button"
                data-selected={isActive}
                onclick={() => execute(e)}
                onmousemove={() => (selected = idx)}
                class="w-full px-4 py-2 flex items-center gap-3 text-left text-sm transition-colors {isActive
                  ? 'bg-lerd-red/10 text-lerd-red'
                  : 'text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-white/4'}"
              >
                <span class="flex-1 truncate">{e.label}</span>
                {#if e.hint}
                  <span class="text-[11px] font-mono text-gray-400 dark:text-gray-500 truncate">{e.hint}</span>
                {/if}
              </button>
            </li>
          {/each}
        {/if}
      </ul>

      <div class="px-4 py-2 border-t border-gray-100 dark:border-lerd-border bg-gray-50/60 dark:bg-white/2 flex items-center gap-3 text-[10px] text-gray-400 dark:text-gray-500">
        <span class="inline-flex items-center gap-1"><kbd class="font-mono">↑↓</kbd> {m.palette_hint_navigate()}</span>
        <span class="inline-flex items-center gap-1"><kbd class="font-mono">↵</kbd> {m.palette_hint_select()}</span>
        <span class="ml-auto inline-flex items-center gap-1"><kbd class="font-mono">⌘K</kbd> {m.palette_hint_toggle()}</span>
      </div>
    </div>
  </div>
{/if}
