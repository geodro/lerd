<script lang="ts">
  import { onMount, tick } from 'svelte';
  import type { Site } from '$stores/sites';
  import {
    listAppLogFiles,
    loadAppLogEntries,
    type AppLogFile,
    type AppLogEntry
  } from '$stores/appLogs';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  let files = $state<AppLogFile[]>([]);
  let selectedFile = $state('');
  let entries = $state<AppLogEntry[]>([]);
  let loading = $state(false);
  let showAll = $state(false);
  let search = $state('');
  let expandedIdx = $state(-1);
  let scrollEl: HTMLDivElement | null = $state(null);

  async function loadFiles() {
    loading = true;
    try {
      const list = await listAppLogFiles(site.domain);
      files = list;
      if (list.length > 0) {
        selectedFile = list[0].name;
        await loadEntries();
      } else {
        entries = [];
      }
    } finally {
      loading = false;
    }
  }

  async function loadEntries() {
    if (!selectedFile) return;
    loading = true;
    try {
      entries = await loadAppLogEntries(site.domain, selectedFile, showAll);
    } finally {
      loading = false;
    }
    await tick();
    if (scrollEl) scrollEl.scrollTop = scrollEl.scrollHeight;
  }

  onMount(() => {
    loadFiles();
  });

  function toggleEntry(i: number) {
    expandedIdx = expandedIdx === i ? -1 : i;
  }

  const filtered = $derived(
    entries.filter((e) => {
      if (!search.trim()) return true;
      const q = search.toLowerCase();
      return (
        (e.message && e.message.toLowerCase().includes(q)) ||
        (e.level && e.level.toLowerCase().includes(q)) ||
        (e.detail && e.detail.toLowerCase().includes(q)) ||
        (e.date && e.date.includes(search))
      );
    })
  );

  const reversed = $derived(filtered.slice().reverse());

  function levelClass(level: string | undefined): string {
    const l = (level || '').toUpperCase();
    if (['ERROR', 'CRITICAL', 'EMERGENCY', 'ALERT'].includes(l))
      return 'bg-red-100 dark:bg-red-500/10 text-red-600 dark:text-red-400';
    if (l === 'WARNING') return 'bg-yellow-100 dark:bg-yellow-500/10 text-yellow-700 dark:text-yellow-400';
    if (['INFO', 'NOTICE'].includes(l)) return 'bg-blue-100 dark:bg-blue-500/10 text-blue-600 dark:text-blue-400';
    return 'bg-gray-100 dark:bg-white/5 text-gray-500 dark:text-gray-400';
  }
</script>

<div class="flex-1 flex flex-col overflow-hidden min-h-0">
  <div class="flex items-center gap-2 px-3 sm:px-5 py-2 shrink-0 border-b border-gray-100 dark:border-lerd-border">
    <select
      bind:value={selectedFile}
      onchange={loadEntries}
      class="text-xs bg-transparent border border-gray-200 dark:border-lerd-border rounded px-2 py-1 text-gray-700 dark:text-gray-300 hover:border-gray-300 dark:hover:border-lerd-muted focus:outline-none focus:border-orange-500/50 cursor-pointer transition-colors"
    >
      {#each files as f (f.name)}
        <option value={f.name}>{f.name}</option>
      {/each}
    </select>

    <div class="flex items-center rounded border border-gray-200 dark:border-lerd-border overflow-hidden shrink-0">
      <button
        onclick={() => {
          showAll = false;
          loadEntries();
        }}
        class="text-[11px] px-2 py-1 transition-colors {!showAll
          ? 'bg-orange-500 text-white'
          : 'text-gray-500 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-white/5'}"
      >{m.sites_appLogs_latest()}</button>
      <button
        onclick={() => {
          showAll = true;
          loadEntries();
        }}
        class="text-[11px] px-2 py-1 transition-colors border-l border-gray-200 dark:border-lerd-border {showAll
          ? 'bg-orange-500 text-white'
          : 'text-gray-500 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-white/5'}"
      >{m.sites_appLogs_all()}</button>
    </div>

    <div class="relative flex-1 max-w-xs">
      <svg class="absolute left-2 top-1/2 -translate-y-1/2 w-3 h-3 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"/>
      </svg>
      <input
        type="text"
        bind:value={search}
        placeholder={m.sites_appLogs_search()}
        class="w-full text-xs bg-transparent border border-gray-200 dark:border-lerd-border rounded pl-7 pr-2 py-1 text-gray-700 dark:text-gray-300 placeholder-gray-400 dark:placeholder-gray-600 hover:border-gray-300 dark:hover:border-lerd-muted focus:outline-none focus:border-orange-500/50 transition-colors"
      />
    </div>

    <button
      onclick={loadEntries}
      class="flex items-center gap-1 text-xs text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 border border-gray-200 dark:border-lerd-border hover:border-gray-300 dark:hover:border-lerd-muted rounded px-2 py-1 transition-colors"
    >
      <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"/>
      </svg>
      {m.common_refresh()}
    </button>

    {#if loading}
      <svg class="animate-spin w-3.5 h-3.5 text-gray-400" fill="none" viewBox="0 0 24 24">
        <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"/>
        <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"/>
      </svg>
    {/if}
  </div>

  <div bind:this={scrollEl} class="flex-1 overflow-y-auto">
    {#if reversed.length === 0 && !loading}
      <div class="text-gray-400 dark:text-gray-600 italic text-xs p-4">{m.sites_appLogs_empty()}</div>
    {/if}
    {#each reversed as entry, i (i + ':' + (entry.date ?? '') + ':' + (entry.message ?? '').slice(0, 40))}
      <div class="border-b border-gray-100 dark:border-lerd-border/50">
        <button
          onclick={() => toggleEntry(i)}
          class="w-full flex items-center gap-3 px-4 py-2 text-left hover:bg-gray-50 dark:hover:bg-white/[0.03] transition-colors"
        >
          <span class="shrink-0 text-[10px] font-bold uppercase px-1.5 py-0.5 rounded leading-tight {levelClass(entry.level)}">
            {entry.level || 'LOG'}
          </span>
          <span class="shrink-0 text-[11px] text-gray-400 font-mono w-[135px]">{entry.date ?? ''}</span>
          <span class="text-xs text-gray-700 dark:text-gray-300 truncate flex-1">{entry.message ?? ''}</span>
          <svg
            class="w-3 h-3 shrink-0 ml-auto text-gray-400 transition-transform duration-150 {expandedIdx === i ? 'rotate-180' : ''}"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"/>
          </svg>
        </button>
        {#if expandedIdx === i}
          <div class="px-4 py-3 bg-gray-50 dark:bg-lerd-bg border-t border-gray-100 dark:border-lerd-border/30 font-mono text-[11px] text-gray-600 dark:text-gray-400 whitespace-pre-wrap break-all max-h-80 overflow-y-auto leading-relaxed">{entry.detail || entry.message || ''}</div>
        {/if}
      </div>
    {/each}
  </div>
</div>
