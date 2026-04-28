<script lang="ts">
  import { onMount } from 'svelte';
  import Modal from '$components/Modal.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import { closeModal } from '$stores/modals';
  import { browseDir } from '$stores/browse';
  import { streamLinkSite } from '$stores/link';
  import { loadSites } from '$stores/sites';
  import { goToTab } from '$stores/route';
  import { m } from '../paraglide/messages.js';

  let currentDir = $state('');
  let dirs = $state<Array<{ name: string; path: string }>>([]);
  let loading = $state(false);
  let linking = $state(false);
  let error = $state('');
  let logs = $state<string[]>([]);
  let scrollEl: HTMLDivElement | null = $state(null);

  async function browse(dir: string) {
    loading = true;
    error = '';
    try {
      const res = await browseDir(dir);
      if (res.error) {
        error = res.error;
      } else {
        currentDir = res.current;
        dirs = res.dirs;
      }
    } finally {
      loading = false;
    }
  }

  async function link() {
    linking = true;
    error = '';
    logs = [];
    try {
      const box: { result: { ok?: boolean; domain?: string; error?: string } | null } = { result: null };
      await streamLinkSite(currentDir, (ev) => {
        if (ev.done) {
          box.result = { ok: ev.ok, domain: ev.domain, error: ev.error };
          return;
        }
        if (ev.line !== undefined) {
          logs = [...logs, ev.line];
          requestAnimationFrame(() => {
            if (scrollEl) scrollEl.scrollTop = scrollEl.scrollHeight;
          });
        }
      });
      const result = box.result;
      if (!result || !result.ok) {
        error = result?.error || m.link_failed();
        return;
      }
      await loadSites();
      closeModal();
      if (result.domain) goToTab('sites', result.domain);
    } catch (e) {
      error = e instanceof Error ? e.message : m.common_failed();
    } finally {
      linking = false;
    }
  }

  onMount(() => {
    browse('');
  });
</script>

<Modal open title={m.link_title()} onclose={closeModal}>
  <div class="px-5 py-3 border-b border-gray-100 dark:border-lerd-border">
    <div class="text-xs text-gray-400 mb-1">{m.link_directory()}</div>
    <div class="text-sm font-mono text-gray-700 dark:text-gray-300 truncate">{currentDir || '...'}</div>
  </div>

  {#if !linking}
    <div class="px-5 py-2 max-h-72 overflow-y-auto">
      {#if loading}
        <div class="py-4 text-center text-xs text-gray-400">{m.common_loading()}</div>
      {:else}
        {#each dirs as d (d.path)}
          <button
            onclick={() => browse(d.path)}
            class="w-full flex items-center gap-2 px-2 py-1.5 text-left text-sm rounded hover:bg-gray-50 dark:hover:bg-white/5 transition-colors {d.name === '..' ? 'text-gray-400' : 'text-gray-700 dark:text-gray-300'}"
          >
            <svg class="w-4 h-4 shrink-0 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z"/>
            </svg>
            <span class="truncate">{d.name}</span>
          </button>
        {/each}
        {#if dirs.length === 0}
          <div class="py-4 text-center text-xs text-gray-400">{m.link_noSubdirs()}</div>
        {/if}
      {/if}
    </div>
  {:else}
    <div class="px-5 py-3">
      <div
        bind:this={scrollEl}
        class="bg-gray-50 dark:bg-black/30 rounded-lg p-3 max-h-56 overflow-y-auto font-mono text-xs text-gray-600 dark:text-gray-400 space-y-0.5"
      >
        {#each logs as line, i (i)}
          <div>{line}</div>
        {/each}
        {#if logs.length === 0}
          <div class="text-gray-400 dark:text-gray-500">{m.link_waitingOutput()}</div>
        {/if}
      </div>
    </div>
  {/if}

  {#if error}
    <div class="px-5 py-2">
      <p class="text-xs text-red-500">{error}</p>
    </div>
  {/if}

  {#snippet footer()}
    {#if !linking}
      <DetailButton onclick={closeModal}>{m.common_cancel()}</DetailButton>
    {/if}
    <DetailButton tone="primary" onclick={link} disabled={linking || loading} loading={linking}>
      {m.link_linkThisDir()}
    </DetailButton>
  {/snippet}
</Modal>
