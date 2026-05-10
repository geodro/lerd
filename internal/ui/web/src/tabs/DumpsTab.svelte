<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import {
    dumps,
    dumpsConnected,
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

  // Local derived: build groups from the always-fresh dumps store using
  // either siteScope (per-site embed) or the global filterSite (standalone
  // view). No store mutation, no race with sibling instances.
  const groups = $derived(
    buildDumpGroups($dumps, scoped ? siteScope : $filterSite, $filterCtx, $filterText)
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
    textTimer = setTimeout(() => filterText.set(v), 100);
  });

  async function onToggle() {
    const target = !$status?.enabled;
    await toggleDumps(target);
  }

  async function onClear() {
    await clearDumps();
  }
</script>

<div class="flex flex-col h-full overflow-hidden">
  <div class="flex items-center gap-2 px-4 py-2 border-b border-gray-200 dark:border-lerd-border bg-white dark:bg-lerd-card flex-wrap">
    <h2 class="font-medium">Dumps</h2>
    <span
      class="px-2 py-0.5 rounded text-xs font-mono"
      class:bg-emerald-100={$status?.enabled}
      class:text-emerald-700={$status?.enabled}
      class:bg-gray-200={!$status?.enabled}
      class:text-gray-600={!$status?.enabled}
    >
      {$status?.enabled ? 'bridge on' : 'bridge off'}
    </span>
    <span class="text-xs text-gray-500">
      {$dumpsConnected ? '● live' : '○ disconnected'}
    </span>

    <button
      type="button"
      class="ml-auto text-xs rounded border border-gray-300 dark:border-lerd-border px-2 py-1 hover:bg-gray-50 dark:hover:bg-lerd-hover"
      onclick={onToggle}
    >
      {$status?.enabled ? 'Disable bridge' : 'Enable bridge'}
    </button>
    <button
      type="button"
      class="text-xs rounded border border-gray-300 dark:border-lerd-border px-2 py-1 hover:bg-gray-50 dark:hover:bg-lerd-hover"
      onclick={onClear}
    >
      Clear
    </button>
  </div>

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
    <select
      class="text-xs px-2 py-1 rounded border border-gray-300 dark:border-lerd-border bg-white dark:bg-lerd-card"
      bind:value={$filterCtx}
    >
      <option value="">All contexts</option>
      <option value="fpm">Web (fpm)</option>
      <option value="cli">CLI</option>
    </select>
  </div>

  <div class="flex-1 overflow-y-auto px-4 py-3">
    {#if groups.length === 0}
      <EmptyState title={$status?.enabled ? 'Waiting for dumps…' : 'Dump bridge is disabled'}>
        {#snippet hint()}
          {$status?.enabled
            ? 'Trigger a dump() or dd() in your PHP code and it will appear here.'
            : 'Run `lerd dump on` or click Enable bridge above to start capturing.'}
        {/snippet}
      </EmptyState>
    {:else}
      {#each groups as group (group.key)}
        <section class="mb-4">
          <header class="flex items-center gap-2 mb-1 sticky top-0 bg-gray-50 dark:bg-lerd-bg py-1 -mx-4 px-4 z-[1]">
            <span class="text-xs font-mono text-gray-500">{new Date(group.ts).toLocaleTimeString()}</span>
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
