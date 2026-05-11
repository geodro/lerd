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
  <div class="flex items-center gap-2 px-4 py-2 border-b border-gray-200 dark:border-lerd-border flex-wrap">
    <input
      class="text-xs px-2 py-1 rounded border border-gray-300 dark:border-lerd-border bg-white dark:bg-lerd-card flex-1 min-w-[140px]"
      placeholder="Search label, file, value…"
      bind:value={textInput}
    />
    {#if !scoped}
      <select
        class="text-xs px-2 py-1 rounded border border-gray-300 dark:border-lerd-border bg-white dark:bg-lerd-card"
        bind:value={$filterSite}
      >
        <option value="">All sites</option>
        {#each $knownSites as site}
          <option value={site}>{site || '(unknown)'}</option>
        {/each}
      </select>
    {/if}
    {#if scoped}
      <select
        class="text-xs px-2 py-1 rounded border border-gray-300 dark:border-lerd-border bg-white dark:bg-lerd-card"
        bind:value={localCtx}
      >
        <option value="">All contexts</option>
        <option value="fpm">Web (fpm)</option>
        <option value="cli">CLI</option>
      </select>
    {:else}
      <select
        class="text-xs px-2 py-1 rounded border border-gray-300 dark:border-lerd-border bg-white dark:bg-lerd-card"
        bind:value={$filterCtx}
      >
        <option value="">All contexts</option>
        <option value="fpm">Web (fpm)</option>
        <option value="cli">CLI</option>
      </select>
    {/if}
    <button
      type="button"
      class="text-xs rounded border border-gray-300 dark:border-lerd-border px-2 py-1 hover:bg-gray-50 dark:hover:bg-lerd-hover"
      onclick={onClear}
    >
      Clear
    </button>
  </div>

  <div class="flex-1 overflow-y-auto px-4 pb-3">
    {#if groups.length === 0}
      {#if !$status?.enabled}
        <div class="px-4 py-10 text-center space-y-3">
          <p class="text-sm text-gray-500 dark:text-gray-400">Dump bridge is disabled</p>
          <p class="text-[11px] text-gray-400 dark:text-gray-500">
            Trigger a dump() or dd() and it will appear here once captures are on.
          </p>
          <button
            type="button"
            disabled={enabling}
            onclick={onEnable}
            class="inline-flex items-center gap-1.5 text-xs rounded border border-emerald-500/40 bg-emerald-50 dark:bg-emerald-900/20 text-emerald-700 dark:text-emerald-300 px-3 py-1.5 hover:border-emerald-500 hover:bg-emerald-100 dark:hover:bg-emerald-900/40 disabled:opacity-50"
          >
            {enabling ? 'Enabling…' : 'Enable dump bridge'}
          </button>
        </div>
      {:else}
        <EmptyState title="Waiting for dumps…">
          {#snippet hint()}
            Trigger a dump() or dd() in your PHP code and it will appear here.
          {/snippet}
        </EmptyState>
      {/if}
    {:else}
      {#each groups as group (group.key)}
        <section class="mb-4">
          <header class="flex items-center gap-2 mb-1 sticky top-0 bg-gray-50 dark:bg-lerd-bg py-1 -mx-4 px-4 z-[1]">
            <span class="text-sm">{group.label}</span>
            <span class="text-xs text-gray-400 ml-auto">{group.events.length} dump{group.events.length === 1 ? '' : 's'}</span>
          </header>
          {#each group.events as ev (ev.id)}
            <DumpEntry event={ev} />
          {/each}
        </section>
      {/each}
    {/if}
  </div>
</div>
