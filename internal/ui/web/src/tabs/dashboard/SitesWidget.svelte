<script lang="ts">
  import DashboardCard from './DashboardCard.svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import StatusDot from '$components/StatusDot.svelte';
  import Badge from '$components/Badge.svelte';
  import Icon from '$components/Icon.svelte';
  import {
    sites,
    sitesLoaded,
    siteWorkerFailing,
    openSiteInBrowser,
    type Site
  } from '$stores/sites';
  import { openLinkModal } from '$stores/modals';
  import { goToTab } from '$stores/route';
  import { accessMode } from '$stores/accessMode';
  import { apiBase } from '$lib/api';
  import { m } from '../../paraglide/messages.js';

  const total = $derived($sites.length);
  const running = $derived($sites.filter((s) => s.fpm_running && !s.paused).length);
  const paused = $derived($sites.filter((s) => s.paused).length);
  const failing = $derived($sites.filter((s) => siteWorkerFailing(s)).length);

  // The backend serialises sites.yaml in registry order — AddSite appends
  // and RemoveSite preserves the rest's positions, so the position of a
  // site in the array reflects when it was registered (oldest first).
  // Reverse and drop paused sites so the dashboard shows the most recently
  // added active projects at the top — paused sites are still visible on
  // the Sites tab.
  const sorted = $derived($sites.filter((s) => !s.paused).reverse());

  function onOpen(s: Site, evt: Event) {
    evt.stopPropagation();
    openSiteInBrowser(s);
  }
</script>

<DashboardCard title={m.dashboard_sites_title()} tone={failing > 0 ? 'critical' : 'default'}>
  {#snippet badge()}
    {#if $sitesLoaded}
      <StatusPill
        tone={failing > 0 ? 'error' : running > 0 ? 'ok' : 'muted'}
        label={m.dashboard_sites_summary({ running, total })}
      />
    {/if}
  {/snippet}

  {#if $sitesLoaded && total === 0}
    <p class="text-sm text-gray-500 dark:text-gray-400">
      {@html m.sites_emptyHint({ cmd: '<code class="bg-gray-100 dark:bg-white/5 px-1 rounded-sm font-mono">lerd park</code>' })}
    </p>
  {:else}
    <div class="space-y-0.5">
      {#each sorted as s (s.domain)}
        <button
          onclick={() => goToTab('sites', s.domain)}
          class="group w-full flex items-center gap-2 px-1.5 py-1.5 rounded-md text-left hover:bg-gray-50 dark:hover:bg-white/4 transition-colors"
        >
          <span class="relative shrink-0 w-4 h-4 flex items-center justify-center">
            {#if s.has_favicon}
              <img src={apiBase + '/api/sites/' + s.domain + '/favicon'} class="w-4 h-4 rounded-xs object-contain" loading="lazy" alt="" />
            {:else}
              <StatusDot color={s.paused ? 'amber' : s.fpm_running ? 'green' : 'gray'} />
            {/if}
          </span>
          <span class="flex-1 min-w-0 text-sm font-medium text-gray-700 dark:text-gray-200 truncate">{s.domain}</span>
          {#if s.framework_label}
            <Badge tone="framework">{s.framework_label}</Badge>
          {/if}
          {#if s.worktrees && s.worktrees.length > 0}
            <span title={m.dashboard_sites_worktrees({ count: s.worktrees.length })} class="shrink-0 inline-flex items-center gap-0.5 text-[10px] font-mono text-violet-500 dark:text-violet-400">
              <svg class="w-3 h-3" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24" aria-hidden="true">
                <path d="M6 3v12M15 6a3 3 0 1 0 6 0a3 3 0 1 0-6 0M3 18a3 3 0 1 0 6 0a3 3 0 1 0-6 0M18 9a9 9 0 0 1-9 9"/>
              </svg>
              {s.worktrees.length}
            </span>
          {/if}
          {#if siteWorkerFailing(s)}
            <span title={m.sites_workerFailing()} class="shrink-0"><StatusDot color="red" size="xs" pulse /></span>
          {/if}
          <span
            role="button"
            tabindex="0"
            title={m.dashboard_sites_openInBrowser()}
            onclick={(e) => onOpen(s, e)}
            onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') onOpen(s, e); }}
            class="shrink-0 w-7 h-7 inline-flex items-center justify-center rounded-sm text-gray-400 hover:text-lerd-red hover:bg-gray-100 dark:hover:bg-white/10 transition-colors cursor-pointer"
          >
            <Icon name="globe" class="w-3.5 h-3.5" />
          </span>
        </button>
      {/each}
    </div>
  {/if}

  {#snippet footer()}
    <div class="flex flex-wrap items-center gap-2">
      {#if $accessMode.loopback}
        <button
          onclick={openLinkModal}
          class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium bg-lerd-red hover:bg-lerd-redhov text-white transition-colors"
        >
          <Icon name="plus" class="w-3.5 h-3.5" />
          {m.dashboard_sites_link()}
        </button>
      {/if}
      <button
        onclick={() => goToTab('sites')}
        class="ml-auto text-xs font-medium text-lerd-red hover:text-lerd-redhov"
      >{m.dashboard_sites_open()}</button>
    </div>
  {/snippet}
</DashboardCard>
