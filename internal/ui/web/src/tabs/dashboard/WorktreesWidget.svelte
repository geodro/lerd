<script lang="ts">
  import DashboardCard from './DashboardCard.svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import StatusDot from '$components/StatusDot.svelte';
  import Icon from '$components/Icon.svelte';
  import {
    sites,
    sitesLoaded,
    removeWorktree,
    stopWorktreeVite,
    type Site
  } from '$stores/sites';
  import { goToTab } from '$stores/route';

  interface FlatWorktree {
    branch: string;
    domain: string;
    siteDomain: string;
    path: string;
    dbIsolated: boolean;
  }

  const allWorktrees = $derived(
    $sites.flatMap((s: Site) =>
      (s.worktrees || []).map((wt) => ({
        branch: wt.branch || '',
        domain: wt.domain || '',
        siteDomain: s.domain || '',
        path: wt.path || '',
        dbIsolated: wt.db_isolated || false,
      }))
    )
  );

  const total = $derived(allWorktrees.length);

  let pending = $state<Record<string, string>>({});

  function onOpen(wt: FlatWorktree, evt: Event) {
    evt.stopPropagation();
    window.open(`https://${wt.domain}`, '_blank');
  }

  async function onStop(wt: FlatWorktree, evt: Event) {
    evt.stopPropagation();
    pending[wt.domain] = 'stopping';
    const r = await stopWorktreeVite(wt.siteDomain, wt.branch);
    if (!r.ok && r.error) console.error('[lerd] stop vite:', r.error);
    delete pending[wt.domain];
  }

  async function onDelete(wt: FlatWorktree, evt: Event) {
    evt.stopPropagation();
    if (!confirm(`Remove worktree "${wt.branch}"? This runs git worktree remove --force.`)) return;
    pending[wt.domain] = 'removing';
    const r = await removeWorktree(wt.siteDomain, wt.branch);
    if (!r.ok && r.error) console.error('[lerd] remove worktree:', r.error);
    delete pending[wt.domain];
  }
</script>

<DashboardCard title="Worktrees">
  {#snippet badge()}
    {#if $sitesLoaded}
      <StatusPill
        tone={total > 0 ? 'ok' : 'muted'}
        label={total === 1 ? '1 worktree' : `${total} worktrees`}
      />
    {/if}
  {/snippet}

  {#if $sitesLoaded && total === 0}
    <p class="text-sm text-gray-500 dark:text-gray-400">
      No active worktrees. Create one with <code class="bg-gray-100 dark:bg-white/5 px-1 rounded font-mono">lerd worktree add</code>
    </p>
  {:else}
    <div class="space-y-0.5">
      {#each allWorktrees as wt (wt.domain)}
        {@const busy = pending[wt.domain]}
        <div
          class="group flex items-center gap-2 px-1.5 py-1.5 rounded-md hover:bg-gray-50 dark:hover:bg-white/[0.04] transition-colors {busy ? 'opacity-50' : ''}"
        >
          <StatusDot color={busy ? 'amber' : 'green'} size="xs" pulse={!!busy} />
          <button
            onclick={() => goToTab('sites', wt.siteDomain)}
            class="flex-1 min-w-0 truncate text-left"
          >
            <span class="text-sm font-medium text-violet-600 dark:text-violet-400">{wt.branch}</span>
            <span class="text-xs text-gray-400 dark:text-gray-500 ml-1">{wt.siteDomain}</span>
          </button>
          {#if wt.dbIsolated}
            <span class="shrink-0 text-[10px] font-mono text-amber-600 dark:text-amber-400" title="Database isolated">DB</span>
          {/if}
          <span
            role="button"
            tabindex="0"
            title="Open in browser"
            onclick={(e) => onOpen(wt, e)}
            onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') onOpen(wt, e); }}
            class="shrink-0 w-7 h-7 inline-flex items-center justify-center rounded text-gray-400 hover:text-lerd-red hover:bg-gray-100 dark:hover:bg-white/10 transition-colors cursor-pointer"
          >
            <Icon name="globe" class="w-3.5 h-3.5" />
          </span>
          <span
            role="button"
            tabindex="0"
            title="Remove worktree"
            onclick={(e) => onDelete(wt, e)}
            onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') onDelete(wt, e); }}
            class="shrink-0 w-7 h-7 inline-flex items-center justify-center rounded text-gray-400 hover:text-red-500 hover:bg-gray-100 dark:hover:bg-white/10 transition-colors cursor-pointer"
          >
            <Icon name="close" class="w-3.5 h-3.5" />
          </span>
        </div>
      {/each}
    </div>
  {/if}

  {#snippet footer()}
    <div class="flex flex-wrap items-center gap-2">
      <button
        onclick={() => goToTab('sites')}
        class="ml-auto text-xs font-medium text-lerd-red hover:text-lerd-redhov"
      >View All Sites</button>
    </div>
  {/snippet}
</DashboardCard>
