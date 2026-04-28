<script lang="ts">
  import ListPanel from '$components/ListPanel.svelte';
  import ActionButton from '$components/ActionButton.svelte';
  import EmptyState from '$components/EmptyState.svelte';
  import Icon from '$components/Icon.svelte';
  import StatusDot from '$components/StatusDot.svelte';
  import LoadingRow from '$components/LoadingRow.svelte';
  import { accessMode } from '$stores/accessMode';
  import { routeRest, goToTab } from '$stores/route';
  import { sites, sitesLoaded, siteWorkerFailing, type Site } from '$stores/sites';
  import { openLinkModal } from '$stores/modals';
  import { apiBase } from '$lib/api';
  import { m } from '../paraglide/messages.js';

  const selected = $derived($routeRest);
  const active = $derived($sites.filter((s) => !s.paused));
  const paused = $derived($sites.filter((s) => s.paused));

  function select(s: Site) {
    goToTab('sites', s.domain);
  }

  function runningWorkerDots(s: Site): string[] {
    const dots: string[] = [];
    if (s.queue_running) dots.push('amber');
    if (s.horizon_running) dots.push('amber');
    if (s.stripe_running) dots.push('violet');
    if (s.schedule_running) dots.push('emerald');
    if (s.reverb_running) dots.push('sky');
    for (const w of s.framework_workers || []) {
      if (w.running) dots.push('indigo');
    }
    return dots;
  }
</script>

{#snippet actions()}
  {#if $accessMode.loopback}
    <ActionButton title={m.sites_linkNew()} tone="accent" onclick={openLinkModal}>
      <Icon name="plus" class="w-3.5 h-3.5" />
    </ActionButton>
  {/if}
{/snippet}

{#snippet parkHint()}
  {@html m.sites_emptyHint({ cmd: '<code class="bg-gray-100 dark:bg-white/5 px-1 rounded">lerd park</code>' })}
{/snippet}

<ListPanel title={m.sites_title()} {actions}>
  {#if !$sitesLoaded}
    <LoadingRow />
  {:else if $sites.length === 0}
    <EmptyState title={m.sites_empty()} hint={parkHint} size="sm" />
  {:else}
    {#each active as s (s.domain)}
      <button
        onclick={() => select(s)}
        class="w-full flex items-center gap-2 px-3 py-2.5 text-left transition-colors border-b border-gray-50 dark:border-lerd-border/50 {selected === s.domain
          ? 'bg-lerd-red/10 text-lerd-red'
          : 'text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-white/[0.03]'}"
      >
        <span class="relative shrink-0 w-4 h-4 flex items-center justify-center">
          {#if s.custom_container}
            <svg class="w-4 h-4 {s.fpm_running ? 'text-violet-500' : 'text-gray-300 dark:text-gray-600'}" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4"/>
            </svg>
          {:else if s.has_favicon}
            <img src={apiBase + '/api/sites/' + s.domain + '/favicon'} class="w-4 h-4 rounded-sm object-contain" loading="lazy" alt="" />
          {:else}
            <StatusDot color={s.fpm_running ? 'green' : 'gray'} />
          {/if}
        </span>
        <span class="flex-1 text-sm truncate">{s.domain}</span>
        {#if s.tls}
          <svg class="w-3 h-3 shrink-0 text-emerald-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z"/>
          </svg>
        {/if}
        {#if s.worktrees && s.worktrees.length > 0}
          <svg class="w-3 h-3 shrink-0 text-violet-400" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">
            <path d="M6 3v12M15 6a3 3 0 1 0 6 0a3 3 0 1 0-6 0M3 18a3 3 0 1 0 6 0a3 3 0 1 0-6 0M18 9a9 9 0 0 1-9 9"/>
          </svg>
        {/if}
        {#if siteWorkerFailing(s)}
          <span title={m.sites_workerFailing()}><StatusDot color="red" size="xs" pulse /></span>
        {/if}
        {#each runningWorkerDots(s) as c, i (i + ':' + c)}
          <StatusDot color={c as 'amber' | 'violet' | 'emerald' | 'sky' | 'indigo'} size="xs" />
        {/each}
      </button>
    {/each}

    {#if paused.length > 0}
      <div class="border-t border-gray-100 dark:border-lerd-border">
        <div class="px-3 py-1.5 text-[10px] font-semibold uppercase tracking-wider text-gray-400 dark:text-gray-500">{m.sites_paused()}</div>
        {#each paused as s (s.domain)}
          <button
            onclick={() => select(s)}
            class="w-full flex items-center gap-2 px-3 py-2 text-left transition-colors border-t border-gray-50 dark:border-lerd-border/50 {selected === s.domain
              ? 'bg-lerd-red/10 text-lerd-red'
              : 'text-gray-400 dark:text-gray-500 hover:bg-gray-50 dark:hover:bg-white/[0.03]'}"
          >
            <svg class="w-3 h-3 shrink-0 opacity-60" fill="currentColor" viewBox="0 0 24 24">
              <path d="M6 5h4v14H6zM14 5h4v14h-4z"/>
            </svg>
            <span class="flex-1 text-sm truncate">{s.domain}</span>
          </button>
        {/each}
      </div>
    {/if}
  {/if}
</ListPanel>
