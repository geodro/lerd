<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
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
    activeWorktreeDomain,
    toggleTLS,
    toggleLANShare
  } from '$stores/sites';
  import { openDomainModal, openWorktreeAddModal, openWorktreeRemoveModal } from '$stores/modals';
  import { accessMode } from '$stores/accessMode';
  import { status, loadStatus } from '$stores/status';
  import { xdebugOn, xdebugOff, type XdebugMode } from '$stores/xdebug';
  import { apiBase } from '$lib/api';
  import ServiceBadgeRow from './ServiceBadgeRow.svelte';
  import DomainMorePill from './DomainMorePill.svelte';
  import LANShareLink from './LANShareLink.svelte';
  import { m } from '../../paraglide/messages.js';

  import type { Snippet } from 'svelte';

  interface Props {
    site: Site;
    tabs?: Snippet;
    activeWorktreeBranch?: string;
    onWorktreeChange?: (branch: string) => void;
    onOpenNginx?: () => void;
  }
  let {
    site,
    tabs,
    activeWorktreeBranch = '',
    onWorktreeChange = () => {},
    onOpenNginx = () => {}
  }: Props = $props();

  let pauseBusy = $state(false);
  let unlinkBusy = $state(false);
  let restartBusy = $state(false);
  let tlsBusy = $state(false);
  let lanBusy = $state(false);
  let xdebugBusy = $state(false);
  let overflowOpen = $state(false);
  let overflowEl: HTMLDivElement | null = $state(null);

  // Xdebug toggles the shared FPM image for this site's PHP version. Only PHP
  // sites on the shared FPM runtime have it (not FrankenPHP or containers).
  const showXdebug = $derived(
    Boolean(site.uses_php) && !site.custom_container && site.runtime !== 'frankenphp'
  );
  const xdebugFpm = $derived(
    site.php_version ? $status.php_fpms.find((f) => f.version === site.php_version) : undefined
  );
  const xdebugEnabled = $derived(Boolean(xdebugFpm?.xdebug_enabled));
  const xdebugMode = $derived((xdebugFpm?.xdebug_mode || 'debug') as XdebugMode);

  async function toggleXdebug() {
    if (!site.php_version || xdebugBusy) return;
    xdebugBusy = true;
    try {
      if (xdebugEnabled) await xdebugOff(site.php_version);
      else await xdebugOn(site.php_version, xdebugMode);
      await loadStatus();
    } finally {
      xdebugBusy = false;
    }
  }

  const activeDomain = $derived(activeWorktreeDomain(site, activeWorktreeBranch));
  const activeWorktree = $derived.by(() => {
    if (!activeWorktreeBranch) return undefined;
    return (site.worktrees || []).find((w) => w.branch === activeWorktreeBranch);
  });
  const activePath = $derived(activeWorktree?.path || site.path || '');
  const activeFrameworkLabel = $derived(activeWorktree?.framework_label || site.framework_label);

  type TabEntry = { branch: string; domain: string; isMain: boolean };
  const tabEntries = $derived.by<TabEntry[]>(() => {
    const main: TabEntry = {
      branch: site.branch || 'main',
      domain: site.domain,
      isMain: true
    };
    const wts: TabEntry[] = (site.worktrees || []).map((wt) => ({
      branch: wt.branch || '',
      domain: wt.domain || '',
      isMain: false
    }));
    return [main, ...wts];
  });
  const showWorktreeTabs = $derived(Boolean(site.branch) && !site.paused);
  const urlEditable = $derived(!site.paused && !activeWorktreeBranch);
  const dnsEnabled = $derived($status.dns?.enabled !== false);
  const tlsToggleable = $derived(urlEditable && dnsEnabled);
  const lanPort = $derived(activeWorktree ? activeWorktree.lan_port ?? 0 : site.lan_port ?? 0);
  const lanURL = $derived(activeWorktree ? activeWorktree.lan_share_url ?? '' : site.lan_share_url ?? '');
  const lanDomain = $derived(activeWorktree ? activeWorktree.domain ?? site.domain : site.domain);
  const lanOn = $derived(Boolean(lanPort));

  const useTLS = $derived(Boolean(site.tls));
  const scheme = $derived(useTLS ? 'https://' : 'http://');

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

  async function flipTLS() {
    if (tlsBusy) return;
    tlsBusy = true;
    try {
      await toggleTLS(site);
      await loadSites();
    } finally {
      tlsBusy = false;
    }
  }

  async function flipLAN() {
    if (lanBusy) return;
    lanBusy = true;
    try {
      await toggleLANShare(site, activeWorktreeBranch);
      await loadSites();
    } finally {
      lanBusy = false;
    }
  }

  function pickWorktree(e: TabEntry) {
    onWorktreeChange(e.isMain ? '' : e.branch);
  }

  function onDocClick(ev: MouseEvent) {
    if (!overflowOpen) return;
    if (overflowEl && !overflowEl.contains(ev.target as Node)) overflowOpen = false;
  }
  function onDocKey(ev: KeyboardEvent) {
    if (ev.key === 'Escape' && overflowOpen) {
      overflowOpen = false;
      ev.stopPropagation();
    }
  }
  onMount(() => {
    document.addEventListener('click', onDocClick, true);
    document.addEventListener('keydown', onDocKey);
  });
  onDestroy(() => {
    document.removeEventListener('click', onDocClick, true);
    document.removeEventListener('keydown', onDocKey);
  });
</script>

<div class="border-b border-gray-100 dark:border-lerd-border shrink-0 @container flex flex-col">
  {#if showWorktreeTabs}
    <div class="flex items-end bg-gray-50/60 dark:bg-white/[0.02]">
      <div class="flex items-center gap-0.5 px-3 pt-3 overflow-x-auto flex-1 min-w-0">
      {#each tabEntries as e (e.isMain ? '__main__' : e.branch)}
        {@const isActive = e.isMain ? activeWorktreeBranch === '' : e.branch === activeWorktreeBranch}
        <div
          class="group flex items-center rounded-t-md border-t border-l border-r transition-colors max-w-56 shrink-0 {isActive
            ? 'bg-white dark:bg-lerd-bg border-gray-200 dark:border-lerd-border'
            : 'bg-transparent border-transparent hover:bg-gray-100/60 dark:hover:bg-white/5'}"
        >
          <button
            type="button"
            onclick={() => pickWorktree(e)}
            title={e.domain}
            class="flex items-center gap-1.5 pl-3 pr-3 py-2.5 text-xs min-w-0 {isActive
              ? 'text-gray-800 dark:text-gray-100 font-medium'
              : 'text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200'}"
          >
            {#if e.isMain}
              <svg
                class="w-3.5 h-3.5 shrink-0 {isActive ? 'text-lerd-red' : 'text-gray-400 dark:text-gray-500'}"
                fill="none"
                stroke="currentColor"
                stroke-width="1.8"
                stroke-linecap="round"
                stroke-linejoin="round"
                viewBox="0 0 24 24"
                aria-label="main"
              >
                <path d="M3 10.5L12 3l9 7.5V20a1 1 0 01-1 1h-4v-6h-8v6H4a1 1 0 01-1-1v-9.5z" />
              </svg>
            {:else}
              <svg
                class="w-3.5 h-3.5 shrink-0 {isActive ? 'text-lerd-red' : 'text-violet-400'}"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
                viewBox="0 0 24 24"
              >
                <path d="M6 3v12M15 6a3 3 0 1 0 6 0a3 3 0 1 0-6 0M3 18a3 3 0 1 0 6 0a3 3 0 1 0-6 0M18 9a9 9 0 0 1-9 9" />
              </svg>
            {/if}
            <span class="font-mono truncate leading-none">{e.branch}</span>
          </button>
          {#if !e.isMain}
            <button
              type="button"
              onclick={(ev) => {
                ev.stopPropagation();
                openWorktreeRemoveModal(site, e.branch);
              }}
              title={m.common_remove() + ' ' + e.branch}
              aria-label={m.common_remove() + ' ' + e.branch}
              class="shrink-0 mr-1 w-4 h-4 flex items-center justify-center rounded-sm text-gray-400 hover:text-red-500 hover:bg-gray-100 dark:hover:bg-white/10 transition-colors"
            >
              <svg class="w-3 h-3" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">
                <path d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          {/if}
        </div>
      {/each}
      {#if !site.paused && site.branch}
        <button
          type="button"
          onclick={() => openWorktreeAddModal(site)}
          class="ml-1 mb-0.5 w-6 h-6 flex items-center justify-center rounded-md text-gray-400 hover:text-lerd-red hover:bg-gray-100 dark:hover:bg-white/5 transition-colors shrink-0"
          title={m.worktreeMgr_add()}
          aria-label={m.worktreeMgr_add()}
        >
          <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
          </svg>
        </button>
      {/if}
      </div>
      {#if activePath}
        <div class="shrink-0 pl-2 pr-3 pb-2 flex items-center text-[11px] leading-none text-gray-500 dark:text-gray-400 min-w-0">
          <span class="font-mono leading-none truncate max-w-[22rem]" title={activePath}>{activePath}</span>
        </div>
      {/if}
    </div>
  {/if}

  <div class="p-3 flex items-center gap-3">
    <div
      class="group flex-1 min-w-0 flex items-center gap-2 h-8 pl-3 pr-2 rounded-full border bg-gray-50 dark:bg-white/[0.03] transition-colors {site.paused
        ? 'border-gray-200 dark:border-lerd-border opacity-70'
        : 'border-gray-200 dark:border-lerd-border hover:bg-white dark:hover:bg-white/[0.06] hover:border-gray-300 dark:hover:border-gray-600 focus-within:bg-white focus-within:border-lerd-red/40'}"
    >
      {#if tlsToggleable}
        <button
          type="button"
          onclick={flipTLS}
          disabled={tlsBusy}
          title={site.tls ? m.sites_controls_httpsToggle_on() : m.sites_controls_httpsToggle_off()}
          aria-label={site.tls ? m.sites_controls_httpsToggle_on() : m.sites_controls_httpsToggle_off()}
          class="shrink-0 -ml-1 p-1 rounded-sm transition-colors disabled:opacity-50 {site.tls
            ? 'text-emerald-500 hover:text-emerald-600 hover:bg-emerald-50 dark:hover:bg-emerald-900/20'
            : 'text-gray-400 dark:text-gray-500 hover:text-gray-600 dark:hover:text-gray-300 hover:bg-gray-100 dark:hover:bg-white/5'}"
        >
          {#if tlsBusy}
            <svg class="animate-spin w-3.5 h-3.5" fill="none" viewBox="0 0 24 24">
              <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
              <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z" />
            </svg>
          {:else if site.tls}
            <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                stroke-width="2"
                d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z"
              />
            </svg>
          {:else}
            <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                stroke-width="2"
                d="M8 11V7a4 4 0 118 0m-4 8v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2z"
              />
            </svg>
          {/if}
        </button>
      {:else if useTLS}
        <span class="shrink-0 -ml-1 p-1 inline-flex items-center text-emerald-500" aria-label="TLS">
          <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
              d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z"
            />
          </svg>
        </span>
      {:else}
        <span class="shrink-0 -ml-1 p-1 inline-flex items-center text-gray-400 dark:text-gray-500" aria-label="No TLS">
          <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
              d="M8 11V7a4 4 0 118 0m-4 8v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2z"
            />
          </svg>
        </span>
      {/if}

      {#if site.has_favicon}
        <img
          src={apiBase + '/api/sites/' + site.domain + '/favicon'}
          class="w-4 h-4 shrink-0 rounded-xs object-contain"
          loading="lazy"
          alt=""
        />
      {:else}
        <svg
          class="w-4 h-4 shrink-0 text-gray-400 dark:text-gray-500"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="2"
            d="M21 12a9 9 0 11-18 0 9 9 0 0118 0zM3.6 9h16.8M3.6 15h16.8M12 3a17 17 0 010 18M12 3a17 17 0 000 18"
          />
        </svg>
      {/if}

      {#if urlEditable}
        <button
          type="button"
          onclick={() => openDomainModal(site)}
          title={m.sites_manageDomains()}
          class="flex items-center min-w-0 flex-1 font-mono cursor-text text-left pt-1.5"
        >
          <span class="text-sm text-gray-400 dark:text-gray-500 shrink-0 leading-none">{scheme}</span>
          <span class="text-sm font-semibold text-gray-800 dark:text-gray-100 truncate leading-none">{activeDomain}</span>
        </button>
      {:else}
        <span title={scheme + activeDomain} class="flex items-baseline min-w-0 flex-1 font-mono pt-1.5">
          <span class="text-sm text-gray-400 dark:text-gray-500 shrink-0 leading-none">{scheme}</span>
          <span class="text-sm font-semibold text-gray-800 dark:text-gray-100 truncate leading-none">{activeDomain}</span>
        </span>
      {/if}

      <DomainMorePill {site} />

      <span class="flex items-center gap-1.5 shrink-0">
        {#if activeFrameworkLabel}
          <Badge tone="framework">{activeFrameworkLabel}</Badge>
        {/if}
        {#if lanOn && lanURL}
          <span class="hidden @md:inline-flex items-center gap-1 text-[10px] text-teal-600 dark:text-teal-400">
            <svg class="w-3 h-3 shrink-0" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">
              <path d="M5 12.55a11 11 0 0114 0M8.5 16.5a5 5 0 017 0M2 8.82a15 15 0 0120 0M12 20h.01" />
            </svg>
            <LANShareLink domain={lanDomain} url={lanURL} siteDomain={site.domain} branch={activeWorktreeBranch} />
          </span>
        {/if}
        {#if site.paused}
          <span class="inline-flex items-center gap-1 text-[11px] text-amber-600 dark:text-amber-400 font-medium">
            <svg class="w-3 h-3" fill="currentColor" viewBox="0 0 24 24">
              <path d="M6 5h4v14H6zM14 5h4v14h-4z" />
            </svg>
            {m.sites_paused().toLowerCase()}
          </span>
        {:else if site.fpm_running}
          <span class="w-2 h-2 rounded-full bg-emerald-500" title={m.common_running()} aria-label={m.common_running()}></span>
        {:else}
          <span class="w-2 h-2 rounded-full bg-gray-300 dark:bg-gray-600" title={m.common_stopped()} aria-label={m.common_stopped()}></span>
        {/if}
      </span>

      {#if !site.paused}
        <button
          type="button"
          onclick={onOpenNginx}
          title={m.sites_nginx_editTitle()}
          aria-label={m.sites_nginx_editTitle()}
          class="shrink-0 -mr-1 p-1 rounded-sm text-gray-400 dark:text-gray-500 hover:text-gray-600 dark:hover:text-gray-300 hover:bg-gray-100 dark:hover:bg-white/5 transition-colors"
        >
          <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">
            <line x1="3" y1="8" x2="21" y2="8" />
            <line x1="3" y1="16" x2="21" y2="16" />
            <line x1="9" y1="6" x2="9" y2="10" />
            <line x1="15" y1="14" x2="15" y2="18" />
          </svg>
        </button>
      {/if}
    </div>

    <div class="flex items-center shrink-0">
      <button
        type="button"
        onclick={() => openSiteInBrowser(site, activeWorktreeBranch)}
        title={m.common_open() + ' — ' + activeDomain}
        aria-label={m.common_open()}
        class="w-8 h-8 flex items-center justify-center rounded-md text-gray-500 dark:text-gray-400 hover:text-lerd-red hover:bg-gray-100 dark:hover:bg-white/5 transition-colors"
      >
        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="2"
            d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14"
          />
        </svg>
      </button>

      {#if !site.paused}
        <button
          type="button"
          onclick={flipLAN}
          disabled={lanBusy}
          title={lanOn ? m.sites_controls_lanToggle_on() : m.sites_controls_lanToggle_off()}
          aria-label={lanOn ? m.sites_controls_lanToggle_on() : m.sites_controls_lanToggle_off()}
          class="hidden @md:flex w-8 h-8 items-center justify-center rounded-md transition-colors disabled:opacity-50 {lanOn
            ? 'text-teal-500 dark:text-teal-400 hover:bg-teal-50 dark:hover:bg-teal-900/20'
            : 'text-gray-500 dark:text-gray-400 hover:text-lerd-red hover:bg-gray-100 dark:hover:bg-white/5'}"
        >
          {#if lanBusy}
            <svg class="animate-spin w-4 h-4" fill="none" viewBox="0 0 24 24">
              <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
              <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z" />
            </svg>
          {:else}
            <svg class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">
              <path d="M5 12.55a11 11 0 0114 0M8.5 16.5a5 5 0 017 0M2 8.82a15 15 0 0120 0M12 20h.01" />
            </svg>
          {/if}
        </button>
      {/if}

      {#if showXdebug && !site.paused}
        <button
          type="button"
          onclick={toggleXdebug}
          disabled={xdebugBusy}
          title={(xdebugEnabled ? m.sites_badges_xdebugOn({ mode: xdebugMode }) : m.sites_badges_xdebugDisabled()) + ' · ' + m.system_php_xdebugHint()}
          aria-label={m.sites_badges_xdebug()}
          aria-pressed={xdebugEnabled}
          class="hidden @md:flex w-8 h-8 items-center justify-center rounded-md transition-colors disabled:opacity-50 {xdebugEnabled
            ? 'text-emerald-500 dark:text-emerald-400 hover:bg-emerald-50 dark:hover:bg-emerald-900/20'
            : 'text-gray-500 dark:text-gray-400 hover:text-lerd-red hover:bg-gray-100 dark:hover:bg-white/5'}"
        >
          {#if xdebugBusy}
            <svg class="animate-spin w-4 h-4" fill="none" viewBox="0 0 24 24">
              <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
              <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z" />
            </svg>
          {:else}
            <svg class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">
              <path d="m8 2 1.88 1.88M14.12 3.88 16 2M9 7.13v-1a3.003 3.003 0 1 1 6 0v1" />
              <path d="M12 20c-3.3 0-6-2.7-6-6v-3a4 4 0 0 1 4-4h4a4 4 0 0 1 4 4v3c0 3.3-2.7 6-6 6zM12 20v-9" />
              <path d="M6.53 9C4.6 8.8 3 7.1 3 5M6 13H2M3 21c0-2.1 1.7-3.9 3.8-4M20.97 5c0 2.1-1.6 3.8-3.5 4M22 13h-4M17.2 17c2.1.1 3.8 1.9 3.8 4" />
            </svg>
          {/if}
        </button>
      {/if}

      {#if $accessMode.loopback}
        <button
          type="button"
          onclick={() => openTerminal(site.domain, activeWorktreeBranch)}
          title={m.sites_openInTerminal()}
          aria-label={m.common_terminal()}
          class="hidden @md:flex w-8 h-8 items-center justify-center rounded-md text-gray-500 dark:text-gray-400 hover:text-lerd-red hover:bg-gray-100 dark:hover:bg-white/5 transition-colors"
        >
          <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
              d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z"
            />
          </svg>
        </button>
      {/if}

      <div class="relative" bind:this={overflowEl}>
        <button
          type="button"
          onclick={() => (overflowOpen = !overflowOpen)}
          aria-label={m.common_moreActions()}
          aria-haspopup="menu"
          aria-expanded={overflowOpen}
          class="w-8 h-8 flex items-center justify-center rounded-md text-gray-500 dark:text-gray-400 hover:text-lerd-red hover:bg-gray-100 dark:hover:bg-white/5 transition-colors"
        >
          <svg class="w-4 h-4" fill="currentColor" viewBox="0 0 24 24">
            <path d="M12 6a2 2 0 100-4 2 2 0 000 4zm0 8a2 2 0 100-4 2 2 0 000 4zm0 8a2 2 0 100-4 2 2 0 000 4z" />
          </svg>
        </button>
        {#if overflowOpen}
          <div
            role="menu"
            class="absolute right-0 top-full mt-1 min-w-[12rem] rounded-md border border-gray-200 dark:border-lerd-border bg-white dark:bg-lerd-bg shadow-lg z-30 py-1"
          >
            {#if !site.paused && (site.uses_php || site.custom_container)}
              <button
                type="button"
                role="menuitem"
                onclick={() => {
                  overflowOpen = false;
                  restart();
                }}
                disabled={restartBusy}
                class="w-full px-3 py-1.5 text-xs text-left flex items-center gap-2 text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-white/5 transition-colors disabled:opacity-50"
              >
                <svg class="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
                  />
                </svg>
                {restartBusy ? '...' : m.sites_restartContainer()}
              </button>
            {/if}
            {#if !activeWorktreeBranch}
              <button
                type="button"
                role="menuitem"
                onclick={() => {
                  overflowOpen = false;
                  togglePause();
                }}
                disabled={pauseBusy}
                class="w-full px-3 py-1.5 text-xs text-left flex items-center gap-2 hover:bg-gray-50 dark:hover:bg-white/5 transition-colors disabled:opacity-50 {site.paused
                  ? 'text-emerald-600 dark:text-emerald-400'
                  : 'text-amber-600 dark:text-amber-400'}"
              >
                {#if site.paused}
                  <svg class="w-3.5 h-3.5 shrink-0" fill="currentColor" viewBox="0 0 24 24"><path d="M6 4.5v15l13-7.5z" /></svg>
                {:else}
                  <svg class="w-3.5 h-3.5 shrink-0" fill="currentColor" viewBox="0 0 24 24"><path d="M6.5 4.5h4v15h-4zM13.5 4.5h4v15h-4z" /></svg>
                {/if}
                {pauseBusy ? '...' : site.paused ? m.sites_resume() : m.sites_pause()}
              </button>
            {/if}
            {#if $accessMode.loopback}
              <button
                type="button"
                role="menuitem"
                onclick={() => {
                  overflowOpen = false;
                  openTerminal(site.domain, activeWorktreeBranch);
                }}
                class="@md:hidden w-full px-3 py-1.5 text-xs text-left flex items-center gap-2 text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-white/5 transition-colors"
              >
                <svg class="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z"
                  />
                </svg>
                {m.common_terminal()}
              </button>
            {/if}
            {#if !site.paused && !activeWorktreeBranch}
              <button
                type="button"
                role="menuitem"
                onclick={() => {
                  overflowOpen = false;
                  openDomainModal(site);
                }}
                class="@md:hidden w-full px-3 py-1.5 text-xs text-left flex items-center gap-2 text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-white/5 transition-colors"
              >
                <svg class="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z"
                  />
                </svg>
                {m.sites_manageDomains()}
              </button>
            {/if}
            {#if !site.paused}
              <button
                type="button"
                role="menuitem"
                onclick={() => {
                  overflowOpen = false;
                  flipLAN();
                }}
                disabled={lanBusy}
                class="@md:hidden w-full px-3 py-1.5 text-xs text-left flex items-center gap-2 hover:bg-gray-50 dark:hover:bg-white/5 transition-colors disabled:opacity-50 {lanOn ? 'text-teal-600 dark:text-teal-400' : 'text-gray-700 dark:text-gray-200'}"
              >
                <svg class="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">
                  <path d="M5 12.55a11 11 0 0114 0M8.5 16.5a5 5 0 017 0M2 8.82a15 15 0 0120 0M12 20h.01" />
                </svg>
                {lanOn ? m.sites_controls_lanToggle_on() : m.sites_controls_lanToggle_off()}
              </button>
            {/if}
            {#if !site.paused || !activeWorktreeBranch}
              <div class="my-1 border-t border-gray-100 dark:border-lerd-border"></div>
            {/if}
            <button
              type="button"
              role="menuitem"
              onclick={() => {
                overflowOpen = false;
                unlink();
              }}
              disabled={unlinkBusy}
              class="w-full px-3 py-1.5 text-xs text-left flex items-center gap-2 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 transition-colors disabled:opacity-50"
            >
              <svg class="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                  d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
                />
              </svg>
              {unlinkBusy ? '...' : m.sites_unlink()}
            </button>
          </div>
        {/if}
      </div>
    </div>
  </div>

  {#if activePath && !showWorktreeTabs}
    <div class="px-2 pb-2 flex items-center text-[11px] text-gray-500 dark:text-gray-400 min-w-0">
      <span class="font-mono truncate" title={activePath}>{activePath}</span>
    </div>
  {/if}

  <div class="px-3 flex flex-col @xl:flex-row justify-between gap-2">
    <div class="pb-3">
      <ServiceBadgeRow {site} />
    </div>
    {#if tabs}
      <div class="flex items-end gap-4 -mb-px pt-2">{@render tabs()}</div>
    {/if}
  </div>
</div>
