<script lang="ts">
  import { openSiteInBrowser, type Site } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  function openWorktree(domain: string) {
    window.open('http://' + domain, '_blank', 'noopener');
  }
</script>

{#if site.worktrees && site.worktrees.length > 0}
  <div class="px-3 sm:px-5 pt-4 pb-1 shrink-0">
    <div class="text-xs font-medium text-gray-500 uppercase tracking-wider mb-3">{m.sites_gitWorktrees()}</div>
    <div class="flex flex-col">
      <div class="flex items-center gap-1.5 py-1">
        <div class="flex items-center shrink-0 text-gray-300 dark:text-gray-600">
          <svg class="w-3.5 h-4" viewBox="0 0 14 16" fill="none">
            <path d="M2 8 L2 16" stroke="currentColor" stroke-width="1.5"/>
          </svg>
        </div>
        <svg class="w-3.5 h-3.5 shrink-0 text-violet-400" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">
          <path d="M6 3v12M15 6a3 3 0 1 0 6 0a3 3 0 1 0-6 0M3 18a3 3 0 1 0 6 0a3 3 0 1 0-6 0M18 9a9 9 0 0 1-9 9"/>
        </svg>
        <span class="text-xs font-mono text-gray-600 dark:text-gray-400 truncate flex-1">{site.branch || 'main'}</span>
        <button
          onclick={() => openSiteInBrowser(site)}
          title={site.domain}
          class="shrink-0 flex items-center gap-1 text-[11px] font-medium text-gray-500 dark:text-gray-400 hover:text-lerd-red dark:hover:text-lerd-red border border-gray-200 dark:border-lerd-border hover:border-lerd-red/40 rounded px-1.5 py-0.5 transition-colors"
        >
          <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14"/>
          </svg>
          <span class="font-mono">{site.domain}</span>
        </button>
      </div>
      {#each site.worktrees as wt, i (wt.domain || wt.branch || i)}
        <div class="flex items-center gap-1.5 py-1">
          <div class="flex items-center shrink-0 text-gray-300 dark:text-gray-600">
            <svg class="w-3.5 h-4" viewBox="0 0 14 16" fill="none">
              <path d={i < (site.worktrees?.length ?? 0) - 1 ? 'M2 0 L2 16' : 'M2 0 L2 10'} stroke="currentColor" stroke-width="1.5"/>
              <path d="M2 10 L10 10" stroke="currentColor" stroke-width="1.5"/>
            </svg>
          </div>
          <svg class="w-3.5 h-3.5 shrink-0 text-violet-400" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">
            <path d="M6 3v12M15 6a3 3 0 1 0 6 0a3 3 0 1 0-6 0M3 18a3 3 0 1 0 6 0a3 3 0 1 0-6 0M18 9a9 9 0 0 1-9 9"/>
          </svg>
          <span class="text-xs font-mono text-gray-600 dark:text-gray-400 truncate flex-1">{wt.branch ?? ''}</span>
          <button
            onclick={() => wt.domain && openWorktree(wt.domain)}
            title={wt.domain}
            class="shrink-0 flex items-center gap-1 text-[11px] font-medium text-gray-500 dark:text-gray-400 hover:text-lerd-red dark:hover:text-lerd-red border border-gray-200 dark:border-lerd-border hover:border-lerd-red/40 rounded px-1.5 py-0.5 transition-colors"
          >
            <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14"/>
            </svg>
            <span class="font-mono">{wt.domain ?? ''}</span>
          </button>
        </div>
      {/each}
    </div>
  </div>
{/if}
