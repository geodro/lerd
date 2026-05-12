<script lang="ts">
  import Badge from '$components/Badge.svelte';
  import {
    type Site,
    pauseSite,
    resumeSite,
    unlinkSite,
    restartSite,
    openSiteInBrowser,
    openTerminal,
    loadSites,
    activeWorktreeDomain
  } from '$stores/sites';
  import { openDomainModal, openWorktreeModal } from '$stores/modals';
  import { accessMode } from '$stores/accessMode';
  import { apiBase } from '$lib/api';
  import ServiceBadgeRow from './ServiceBadgeRow.svelte';
  import DomainMorePill from './DomainMorePill.svelte';
  import WorktreePicker from './WorktreePicker.svelte';
  import { m } from '../../paraglide/messages.js';

  import type { Snippet } from 'svelte';

  interface Props {
    site: Site;
    tabs?: Snippet;
    activeWorktreeBranch?: string;
    onWorktreeChange?: (branch: string) => void;
  }
  let { site, tabs, activeWorktreeBranch = '', onWorktreeChange = () => {} }: Props = $props();

  let pauseBusy = $state(false);
  let unlinkBusy = $state(false);
  let restartBusy = $state(false);

  const activeDomain = $derived(activeWorktreeDomain(site, activeWorktreeBranch));
  const activeWorktree = $derived.by(() => {
    if (!activeWorktreeBranch) return undefined;
    return (site.worktrees || []).find((w) => w.branch === activeWorktreeBranch);
  });
  const activePath = $derived(activeWorktree?.path || site.path);
  const activeFrameworkLabel = $derived(activeWorktree?.framework_label || site.framework_label);

  async function togglePause() {
    pauseBusy = true;
    try {
      await (site.paused ? resumeSite(site.domain) : pauseSite(site.domain));
      await loadSites();
    } finally {
      pauseBusy = false;
    }
  }

  async function unlink() {
    if (!confirm(m.sites_confirmUnlink({ domain: site.domain }))) return;
    unlinkBusy = true;
    try {
      const res = await unlinkSite(site.domain);
      if (!res.ok) alert(m.sites_unlinkFailed({ error: res.error || '' }));
      await loadSites();
    } finally {
      unlinkBusy = false;
    }
  }

  async function restart() {
    restartBusy = true;
    try {
      const res = await restartSite(site.domain);
      if (!res.ok) alert(m.sites_restartFailed({ error: res.error || '' }));
    } finally {
      restartBusy = false;
    }
  }

</script>

<div class="px-3 sm:px-5 pt-4 border-b border-gray-100 dark:border-lerd-border shrink-0 @container flex flex-col gap-2">
  <div class="flex flex-col @md:flex-row justify-between items-start gap-2">
    <div class="flex items-center gap-2 flex-wrap">
      {#if site.has_favicon}
        <img src={apiBase + '/api/sites/' + site.domain + '/favicon'} class="w-5 h-5 rounded-xs object-contain" loading="lazy" alt="" />
      {/if}
      <a
              href={(site.tls && !activeWorktreeBranch ? 'https://' : 'http://') + activeDomain}
              onclick={(e) => {
            e.preventDefault();
            openSiteInBrowser(site, activeWorktreeBranch);
          }}
              class="font-semibold text-lerd-red hover:text-lerd-redhov transition-colors"
      >{activeDomain}</a>

      <DomainMorePill {site} />

      {#if !site.paused && !activeWorktreeBranch}
        <button
                onclick={() => openDomainModal(site)}
                class="text-gray-400 hover:text-lerd-red transition-colors"
                title={m.sites_manageDomains()}
        >
          <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z"/>
          </svg>
        </button>
      {/if}
      {#if !site.paused}
        {#if site.fpm_running}
          <Badge tone="running" dot>{m.common_running().toLowerCase()}</Badge>
        {:else}
          <Badge tone="stopped" dot>{m.common_stopped().toLowerCase()}</Badge>
        {/if}
        <button
                onclick={restart}
                disabled={restartBusy}
                title={m.sites_restartContainer()}
                class="text-gray-400 hover:text-lerd-red transition-colors disabled:opacity-50"
        >
          {#if restartBusy}
            <svg class="animate-spin w-3.5 h-3.5" fill="none" viewBox="0 0 24 24">
              <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"/>
              <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"/>
            </svg>
          {:else}
            <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"/>
            </svg>
          {/if}
        </button>
      {/if}

      {#if activeFrameworkLabel}
        <Badge tone="framework">{activeFrameworkLabel}</Badge>
      {/if}

      {#if site.paused}
        <Badge tone="paused">
          <svg class="w-3 h-3" fill="currentColor" viewBox="0 0 24 24">
            <path d="M6 5h4v14H6zM14 5h4v14h-4z"/>
          </svg>
          {m.sites_paused().toLowerCase()}
        </Badge>
      {/if}
    </div>
    <div class="flex items-center gap-2 flex-wrap justify-start @sm:justify-end @sm:p-0 @sm-mx-3">
      <button
              onclick={togglePause}
              disabled={pauseBusy}
              class="text-xs border rounded-sm px-2 py-1 transition-colors disabled:opacity-50 {site.paused
          ? 'border-emerald-300 dark:border-emerald-700 text-emerald-600 dark:text-emerald-400 hover:bg-emerald-50 dark:hover:bg-emerald-900/20'
          : 'border-amber-300 dark:border-amber-700 text-amber-600 dark:text-amber-400 hover:bg-amber-50 dark:hover:bg-amber-900/20'}"
      >{pauseBusy ? '...' : site.paused ? m.sites_resume() : m.sites_pause()}</button>
      <button
              onclick={unlink}
              disabled={unlinkBusy}
              class="text-xs border border-red-300 dark:border-red-700 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 rounded-sm px-2 py-1 transition-colors disabled:opacity-50"
      >{unlinkBusy ? '...' : m.sites_unlink()}</button>
      <button
              onclick={() => openSiteInBrowser(site, activeWorktreeBranch)}
              class="flex items-center gap-1.5 text-xs border border-gray-200 dark:border-lerd-border text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-white/5 rounded-sm px-2 py-1 transition-colors"
              title={activeDomain}
      >
        <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14"/>
        </svg>
        {m.common_open()}
      </button>
      {#if $accessMode.loopback}
        <button
                onclick={() => openTerminal(site.domain, activeWorktreeBranch)}
                class="flex items-center gap-1.5 text-xs border border-gray-200 dark:border-lerd-border text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-white/5 rounded-sm px-2 py-1 transition-colors"
                title={m.sites_openInTerminal()}
        >
          <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z"/>
          </svg>
          {m.common_terminal()}
        </button>
      {/if}
    </div>
  </div>

  <div class="text-xs text-gray-400 font-mono flex flex-col @lg:flex-row gap-2">
    <span class="truncate">{activePath}</span>
    <div class="flex items-center gap-2 shrink-0">
      <WorktreePicker {site} activeBranch={activeWorktreeBranch} onchange={onWorktreeChange} />
      {#if !site.paused}
        <button
                onclick={() => openWorktreeModal(site)}
                class="text-gray-400 hover:text-lerd-red transition-colors"
                title={m.worktreeMgr_title()}
                aria-label={m.worktreeMgr_title()}
        >
          <svg class="w-3 h-3" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">
            <path d="M6 3v12M15 6a3 3 0 1 0 6 0a3 3 0 1 0-6 0M3 18a3 3 0 1 0 6 0a3 3 0 1 0-6 0M18 9a9 9 0 0 1-9 9"/>
          </svg>
        </button>
      {/if}
    </div>
  </div>

  <div class="flex flex-col @xl:flex-row justify-between gap-2">
    <div class="pb-2">
      <ServiceBadgeRow {site} />
    </div>
    {#if tabs}
      <div class="flex items-center gap-5 -mb-px">{@render tabs()}</div>
    {/if}
  </div>
</div>
