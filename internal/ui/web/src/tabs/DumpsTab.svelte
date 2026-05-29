<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import {
    dumps,
    status,
    filterSite,
    filterCtx,
    filterText,
    knownSites,
    startDumpsStream,
    stopDumpsStream,
    refreshStatus,
    clearDumps,
    toggleDumps,
    buildDumpGroups
  } from '$stores/dumps';
  import DumpEntry from '$components/DumpEntry.svelte';
  import EmptyState from '$components/EmptyState.svelte';
  import Dropdown from '$components/Dropdown.svelte';
  import { m } from '../paraglide/messages.js';

  interface Props {
    // siteScope pins the site filter for this view. When set, the site
    // picker is hidden and only events whose ctx.site matches the scope
    // are rendered. Other filters (ctx, text) remain user-controlled and
    // the global filterSite store stays untouched.
    siteScope?: string;
  }
  let { siteScope = '' }: Props = $props();
  const scoped = $derived(siteScope !== '');

  // When scoped (embedded in SiteDetail), search and context filters are
  // local-only — the global filterCtx / filterText writables stay
  // untouched so the System > Dump bridge view doesn't inherit a stale
  // search and vice versa. The unscoped instance keeps using the global
  // stores so user choices persist between visits.
  let localCtx = $state<'' | 'fpm' | 'cli'>('');
  let localText = $state('');

  const effectiveCtx = $derived(scoped ? localCtx : $filterCtx);
  const effectiveText = $derived(scoped ? localText : $filterText);

  const groups = $derived(
    buildDumpGroups($dumps, scoped ? siteScope : $filterSite, effectiveCtx, effectiveText, scoped)
  );

  onMount(() => {
    startDumpsStream();
    void refreshStatus();
  });

  onDestroy(() => {
    stopDumpsStream();
  });

  let textInput = $state('');
  let textTimer: ReturnType<typeof setTimeout> | null = null;
  $effect(() => {
    const v = textInput;
    if (textTimer) clearTimeout(textTimer);
    textTimer = setTimeout(() => {
      if (scoped) {
        localText = v;
      } else {
        filterText.set(v);
      }
    }, 100);
  });

  async function onClear() {
    await clearDumps();
  }

  let enabling = $state(false);
  async function onEnable() {
    if (enabling) return;
    enabling = true;
    try {
      await toggleDumps(true);
      await refreshStatus();
    } finally {
      enabling = false;
    }
  }
</script>

<div class="flex flex-col h-full overflow-hidden">
  <div class="flex items-center gap-2 px-3 py-3 border-b border-gray-200 dark:border-lerd-border flex-wrap">
    <input
      class="text-xs px-2 py-1 rounded-sm border border-gray-300 dark:border-lerd-border bg-white dark:bg-lerd-card flex-1 min-w-[140px]"
      placeholder={m.dumps_searchPlaceholder()}
      bind:value={textInput}
    />
    {#if !scoped}
      <Dropdown
        value={$filterSite}
        options={[
          { value: '', label: m.dumps_filter_allSites() },
          ...$knownSites.map((s) => ({ value: s, label: s || m.dumps_unknownSite() }))
        ]}
        onchange={(v) => filterSite.set(v)}
      />
    {/if}
    {#if scoped}
      <Dropdown
        value={localCtx}
        options={[
          { value: '', label: m.dumps_filter_allContexts() },
          { value: 'fpm', label: m.dumps_filter_web() },
          { value: 'cli', label: m.dumps_filter_cli() }
        ]}
        onchange={(v) => (localCtx = v as '' | 'fpm' | 'cli')}
      />
    {:else}
      <Dropdown
        value={$filterCtx}
        options={[
          { value: '', label: m.dumps_filter_allContexts() },
          { value: 'fpm', label: m.dumps_filter_web() },
          { value: 'cli', label: m.dumps_filter_cli() }
        ]}
        onchange={(v) => filterCtx.set(v as '' | 'fpm' | 'cli')}
      />
    {/if}
    <button
      type="button"
      class="text-xs rounded-sm border border-gray-300 dark:border-lerd-border px-2 py-1 hover:bg-gray-50 dark:hover:bg-white/5"
      onclick={onClear}
    >
      {m.common_clear()}
    </button>
  </div>

  <div class="flex-1 overflow-y-auto px-3 pb-3">
    {#if groups.length === 0}
      {#if !$status?.enabled}
        <div class="px-3 py-10 text-center space-y-3">
          <p class="text-sm text-gray-500 dark:text-gray-400">{m.dumps_disabled_title()}</p>
          <p class="text-[11px] text-gray-400 dark:text-gray-500">
            {m.dumps_disabled_body()}
          </p>
          <button
            type="button"
            disabled={enabling}
            onclick={onEnable}
            class="inline-flex items-center gap-1.5 text-xs rounded-sm border border-emerald-500/40 bg-emerald-50 dark:bg-emerald-900/20 text-emerald-700 dark:text-emerald-300 px-3 py-1.5 hover:border-emerald-500 hover:bg-emerald-100 dark:hover:bg-emerald-900/40 disabled:opacity-50"
          >
            {enabling ? m.dumps_enabling() : m.dumps_enable()}
          </button>
        </div>
      {:else}
        <EmptyState title={m.dumps_waiting_title()}>
          {#snippet hint()}
            {m.dumps_waiting_body()}
          {/snippet}
        </EmptyState>
      {/if}
    {:else}
      {#each groups as group (group.key)}
        <section class="mb-4">
          <header class="flex items-center gap-2 mb-1 sticky top-0 bg-gray-50 dark:bg-lerd-bg py-1 -mx-3 px-3 z-1">
            <span class="text-sm">{group.label}</span>
            <span class="text-xs text-gray-400 ml-auto">{m.dumps_groupCount({ count: group.events.length })}</span>
          </header>
          {#each group.events as ev (ev.id)}
            <DumpEntry event={ev} />
          {/each}
        </section>
      {/each}
    {/if}
  </div>
</div>
