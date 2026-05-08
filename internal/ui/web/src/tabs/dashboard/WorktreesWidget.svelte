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
    siteTls: boolean;
    path: string;
    dbIsolated: boolean;
  }

  const allWorktrees = $derived(
    $sites.flatMap((s: Site) =>
      (s.worktrees || []).map((wt) => ({
        branch: wt.branch || '',
        domain: wt.domain || '',
        siteDomain: s.domain || '',
        siteTls: Boolean(s.tls),
        path: wt.path || '',
        dbIsolated: wt.db_isolated || false,
      }))
    )
  );

  const total = $derived(allWorktrees.length);

  let pending = $state<Record<string, string>>({});
  let errors = $state<Record<string, string>>({});

  function onOpen(wt: FlatWorktree, evt: Event) {
    evt.stopPropagation();
    const scheme = wt.siteTls ? 'https' : 'http';
    window.open(`${scheme}://${wt.domain}`, '_blank');
  }

  async function onStop(wt: FlatWorktree, evt: Event) {
    evt.stopPropagation();
    delete errors[wt.domain];
    pending[wt.domain] = 'stopping';
    const r = await stopWorktreeVite(wt.siteDomain, wt.branch);
    if (!r.ok && r.error) errors[wt.domain] = r.error;
    delete pending[wt.domain];
  }

  async function onDelete(wt: FlatWorktree, evt: Event) {
    evt.stopPropagation();
    if (!confirm(`Remove worktree "${wt.branch}"? This runs git worktree remove --force.`)) return;
    delete errors[wt.domain];
    pending[wt.domain] = 'removing';
    await stopWorktreeVite(wt.siteDomain, wt.branch);
    const r = await removeWorktree(wt.siteDomain, wt.branch);
    if (!r.ok && r.error) errors[wt.domain] = r.error;
    delete pending[wt.domain];
  }
</script>

{#if total > 0}
<DashboardCard title="Worktrees">
  {#snippet badge()}
    <StatusPill tone="ok" label={total === 1 ? '1 worktree' : `${total} worktrees`} />
  {/snippet}

  <div class="space-y-0.5">
    {#each allWorktrees as wt (wt.domain)}
      {@const busy = pending[wt.domain]}
      {@const error = errors[wt.domain]}
      <div
        class="group flex items-center gap-2 px-1.5 py-1.5 rounded-md hover:bg-gray-50 dark:hover:bg-white/[0.04] transition-colors {busy ? 'opacity-50' : ''}"
      >
        <StatusDot color={error ? 'red' : busy ? 'amber' : 'green'} size="xs" pulse={!!busy} />
        <button
          onclick={() => goToTab('sites', wt.siteDomain)}
          class="flex-1 min-w-0 truncate text-left"
        >
          <span class="text-sm font-medium text-violet-600 dark:text-violet-400">{wt.branch}</span>
          <span class="text-xs text-gray-400 dark:text-gray-500 ml-1">{wt.siteDomain}</span>
          {#if error}
            <span class="block text-[11px] text-red-500 dark:text-red-400 truncate" title={error}>{error}</span>
          {/if}
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
          title="Stop Vite worker"
          onclick={(e) => onStop(wt, e)}
          onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') onStop(wt, e); }}
          class="shrink-0 w-7 h-7 inline-flex items-center justify-center rounded text-gray-400 hover:text-amber-500 hover:bg-gray-100 dark:hover:bg-white/10 transition-colors cursor-pointer"
        >
          <Icon name="stop" class="w-3.5 h-3.5" />
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

  {#snippet footer()}
    <div class="flex flex-wrap items-center gap-2">
      <button
        onclick={() => goToTab('sites')}
        class="ml-auto text-xs font-medium text-lerd-red hover:text-lerd-redhov"
      >View All Sites</button>
    </div>
  {/snippet}
</DashboardCard>
{/if}
