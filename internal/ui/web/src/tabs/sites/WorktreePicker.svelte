<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import type { Site } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
    activeBranch: string;
    onchange: (branch: string) => void;
  }
  let { site, activeBranch, onchange }: Props = $props();

  let open = $state(false);
  let pickerEl: HTMLDivElement | null = $state(null);

  type Entry = { branch: string; domain: string; isMain: boolean };

  const entries = $derived.by<Entry[]>(() => {
    const main: Entry = {
      branch: site.branch || 'main',
      domain: site.domain,
      isMain: true
    };
    const wts: Entry[] = (site.worktrees || []).map((wt) => ({
      branch: wt.branch || '',
      domain: wt.domain || '',
      isMain: false
    }));
    return [main, ...wts];
  });

  const active = $derived(
    entries.find((e) => (e.isMain ? activeBranch === '' : e.branch === activeBranch)) || entries[0]
  );

  function pick(e: Entry) {
    onchange(e.isMain ? '' : e.branch);
    open = false;
  }

  function openInBrowser(domain: string, useTLS: boolean) {
    const url = (useTLS ? 'https://' : 'http://') + domain;
    window.open(url, '_blank', 'noopener');
  }

  function onDocClick(ev: MouseEvent) {
    if (!open) return;
    if (pickerEl && !pickerEl.contains(ev.target as Node)) open = false;
  }

  function onKey(ev: KeyboardEvent) {
    if (ev.key === 'Escape' && open) {
      open = false;
      ev.stopPropagation();
    }
  }

  onMount(() => {
    document.addEventListener('click', onDocClick, true);
    document.addEventListener('keydown', onKey);
  });
  onDestroy(() => {
    document.removeEventListener('click', onDocClick, true);
    document.removeEventListener('keydown', onKey);
  });

  const hasWorktrees = $derived(entries.length > 1);
</script>

<div bind:this={pickerEl} class="relative inline-flex shrink-0">
  {#if hasWorktrees}
    <button
      type="button"
      onclick={() => (open = !open)}
      class="flex items-center gap-1 text-violet-500 dark:text-violet-400 hover:text-lerd-red dark:hover:text-lerd-red transition-colors"
      title={m.sites_gitWorktrees()}
    >
      <span class="font-mono">git:({active.branch})</span>
      <span class="text-[10px] text-gray-400 dark:text-gray-500">{entries.length}</span>
      <svg class="w-3 h-3 transition-transform {open ? 'rotate-180' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"/>
      </svg>
    </button>
  {:else if site.branch}
    <span class="flex items-center gap-1 text-violet-500 dark:text-violet-400">
      <span class="font-mono">git:({site.branch})</span>
    </span>
  {/if}

  {#if open && hasWorktrees}
    <div
      class="absolute left-0 top-[calc(100%+4px)] z-20 min-w-[20rem] max-w-md rounded-sm border border-gray-200 dark:border-lerd-border bg-white dark:bg-lerd-bg shadow-lg overflow-hidden"
    >
      <div class="max-h-72 overflow-y-auto py-1">
        {#each entries as e (e.isMain ? '__main__' : e.branch)}
          {@const selected = e.isMain ? activeBranch === '' : e.branch === activeBranch}
          <div class="flex items-center gap-2 px-2 py-1.5 text-xs hover:bg-gray-50 dark:hover:bg-white/5 transition-colors {selected ? 'bg-lerd-red/5 dark:bg-lerd-red/10' : ''}">
            <button
              type="button"
              onclick={() => pick(e)}
              class="flex-1 flex items-center gap-2 min-w-0 text-left"
            >
              <svg class="w-3.5 h-3.5 shrink-0 {selected ? 'text-lerd-red' : 'text-violet-400'}" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">
                <path d="M6 3v12M15 6a3 3 0 1 0 6 0a3 3 0 1 0-6 0M3 18a3 3 0 1 0 6 0a3 3 0 1 0-6 0M18 9a9 9 0 0 1-9 9"/>
              </svg>
              <span class="font-mono truncate {selected ? 'text-lerd-red font-semibold' : 'text-gray-700 dark:text-gray-300'}">{e.branch}</span>
              {#if e.isMain}
                <span class="shrink-0 text-[10px] uppercase tracking-wider text-gray-400 dark:text-gray-500">main</span>
              {/if}
              <span class="ml-auto pl-2 font-mono text-[11px] text-gray-400 dark:text-gray-500 truncate">{e.domain}</span>
            </button>
            <button
              type="button"
              onclick={(ev) => {
                ev.stopPropagation();
                openInBrowser(e.domain, Boolean(site.tls) && e.isMain);
              }}
              title={e.domain}
              class="shrink-0 text-gray-400 hover:text-lerd-red transition-colors p-1"
            >
              <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14"/>
              </svg>
            </button>
          </div>
        {/each}
      </div>
    </div>
  {/if}
</div>
