<script lang="ts">
  import StatusPill from '$components/StatusPill.svelte';
  import ButtonMenu, { type ButtonMenuAction } from '$components/ButtonMenu.svelte';
  import DetailTabs, { type TabItem } from '$components/DetailTabs.svelte';
  import LogViewer from '$components/LogViewer.svelte';
  import PhpIniTab from './PhpIniTab.svelte';
  import { status, loadStatus, fpmRunning } from '$stores/status';
  import { setDefaultPhp, startPhp, stopPhp, removePhp } from '$stores/phpVersions';
  import { sites, sitesByPhp } from '$stores/sites';
  import { xdebugOn, xdebugOff, XDEBUG_MODES, type XdebugMode } from '$stores/xdebug';
  import { goToTab } from '$stores/route';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    version: string;
  }
  let { version }: Props = $props();

  const running = $derived(fpmRunning(version));
  const isDefault = $derived($status.php_default === version);
  const siteCount = $derived($sitesByPhp.get(version) ?? 0);
  const fpm = $derived($status.php_fpms.find((f) => f.version === version));
  const xdebugEnabled = $derived(Boolean(fpm?.xdebug_enabled));
  const xdebugMode = $derived<XdebugMode>((fpm?.xdebug_mode as XdebugMode) || 'debug');
  const container = $derived('lerd-php' + version.replace('.', '') + '-fpm');
  const sitesUsing = $derived($sites.filter((s) => s.php_version === version));

  let defaultBusy = $state(false);
  let fpmBusy = $state(false);
  let removeBusy = $state(false);
  let xdebugBusy = $state(false);
  let removeError = $state('');
  let xdebugMenuOpen = $state(false);
  let xdebugRootEl: HTMLDivElement | undefined = $state();

  // The parent (PhpPage) no longer wraps us in {#key version}; reset
  // per-version transient state when the version prop changes so a stale
  // "removeError" or open xdebug menu doesn't leak across tabs.
  $effect(() => {
    version;
    removeError = '';
    xdebugMenuOpen = false;
  });

  function closeXdebugMenu() {
    xdebugMenuOpen = false;
  }

  function onXdebugDocClick(e: MouseEvent) {
    if (!xdebugRootEl) return;
    if (!xdebugRootEl.contains(e.target as Node)) closeXdebugMenu();
  }

  function onXdebugDocKey(e: KeyboardEvent) {
    if (e.key === 'Escape') closeXdebugMenu();
  }

  $effect(() => {
    if (!xdebugMenuOpen) return;
    document.addEventListener('mousedown', onXdebugDocClick);
    document.addEventListener('keydown', onXdebugDocKey);
    return () => {
      document.removeEventListener('mousedown', onXdebugDocClick);
      document.removeEventListener('keydown', onXdebugDocKey);
    };
  });

  type TabId = 'logs' | 'sites' | 'config';
  let active = $state<TabId>('logs');
  const tabs = $derived<TabItem<TabId>[]>([
    { id: 'logs', label: m.services_tabs_logs(), hidden: !running },
    { id: 'sites', label: m.system_php_sites() },
    { id: 'config', label: m.services_tabs_tuning() }
  ]);

  $effect(() => {
    if (active === 'logs' && !running) active = 'sites';
  });

  async function onSetDefault() {
    defaultBusy = true;
    try {
      await setDefaultPhp(version);
      await loadStatus();
    } finally {
      defaultBusy = false;
    }
  }

  async function onToggleFpm() {
    fpmBusy = true;
    try {
      await (running ? stopPhp(version) : startPhp(version));
      await loadStatus();
    } finally {
      fpmBusy = false;
    }
  }

  async function onRemove() {
    removeBusy = true;
    removeError = '';
    try {
      const r = await removePhp(version);
      if (!r) removeError = m.common_failed();
      await loadStatus();
    } finally {
      removeBusy = false;
    }
  }

  async function onToggleXdebug() {
    xdebugBusy = true;
    try {
      if (xdebugEnabled) {
        await xdebugOff(version);
      } else {
        await xdebugOn(version, xdebugMode);
      }
      await loadStatus();
    } finally {
      xdebugBusy = false;
    }
  }

  async function onSetXdebugMode(e: Event) {
    const mode = (e.target as HTMLSelectElement).value as XdebugMode;
    if (mode === xdebugMode) return;
    xdebugBusy = true;
    try {
      await xdebugOn(version, mode);
      await loadStatus();
    } finally {
      xdebugBusy = false;
    }
  }

  const headerBusy = $derived(fpmBusy || defaultBusy || removeBusy);

  const headerActions = $derived.by<ButtonMenuAction[]>(() => {
    if (isDefault) return [];
    const acts: ButtonMenuAction[] = [];
    if (running) {
      acts.push({
        id: 'stop',
        icon: stopIcon,
        label: m.common_stop(),
        title: siteCount > 0 ? m.system_php_stopWarn({ count: siteCount }) : m.system_php_stopTitle(),
        disabled: fpmBusy,
        onclick: onToggleFpm
      });
    } else {
      acts.push({
        id: 'start',
        tone: 'success',
        icon: startIcon,
        label: m.common_start(),
        title: m.system_php_startTitle(),
        disabled: fpmBusy,
        onclick: onToggleFpm
      });
    }
    acts.push({
      id: 'set-default',
      icon: starIcon,
      label: m.system_php_setDefault(),
      disabled: defaultBusy,
      onclick: onSetDefault
    });
    acts.push({
      id: 'remove',
      tone: 'danger',
      icon: trashIcon,
      label: m.common_remove(),
      title: siteCount > 0 ? m.system_php_removeWarn({ count: siteCount }) : m.system_php_removeTitle(),
      disabled: removeBusy,
      onclick: onRemove
    });
    return acts;
  });
</script>

{#snippet startIcon()}
  <svg class="w-3.5 h-3.5" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>
{/snippet}
{#snippet stopIcon()}
  <svg class="w-3.5 h-3.5" fill="currentColor" viewBox="0 0 24 24"><rect x="6" y="6" width="12" height="12" rx="1"/></svg>
{/snippet}
{#snippet starIcon()}
  <svg class="w-3.5 h-3.5" fill="currentColor" viewBox="0 0 20 20"><path d="M10 1.5l2.6 5.27 5.82.85-4.21 4.1.99 5.78L10 14.77l-5.2 2.73.99-5.78L1.58 7.62l5.82-.85L10 1.5z"/></svg>
{/snippet}
{#snippet trashIcon()}
  <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg>
{/snippet}

<div
  class="flex flex-wrap items-center justify-between gap-y-2 px-3 sm:px-5 py-4 border-b border-gray-100 dark:border-lerd-border shrink-0"
>
  <div class="flex items-center gap-3 flex-wrap">
    <StatusPill tone={running ? 'ok' : 'muted'} label={running ? m.common_running() : m.common_stopped()} />
  </div>
  <div class="flex items-center gap-2 flex-wrap">
    <div bind:this={xdebugRootEl} class="relative inline-flex">
      <button
        type="button"
        onclick={onToggleXdebug}
        disabled={xdebugBusy}
        aria-pressed={xdebugEnabled}
        title={xdebugEnabled ? 'Disable Xdebug' : 'Enable Xdebug'}
        class="inline-flex items-center gap-1.5 px-3 py-1.5 border border-gray-200 dark:border-lerd-border transition-colors text-xs font-medium text-gray-700 dark:text-gray-200 disabled:opacity-50 {xdebugEnabled
          ? 'rounded-l-lg border-r-0 bg-emerald-50/60 dark:bg-emerald-900/15 hover:bg-emerald-50 dark:hover:bg-emerald-900/25'
          : 'rounded-lg bg-white dark:bg-lerd-card hover:bg-gray-50 dark:hover:bg-white/5'}"
      >
        {#if xdebugBusy}
          <svg class="w-2.5 h-2.5 animate-spin text-amber-500" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-30" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
            <path class="opacity-90" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z" />
          </svg>
        {:else}
          <span class="shrink-0 w-2 h-2 rounded-full {xdebugEnabled ? 'bg-emerald-500' : 'border border-gray-300 dark:border-gray-600 bg-transparent'}"></span>
        {/if}
        <span>{m.system_php_xdebug()}</span>
      </button>
      {#if xdebugEnabled}
        <button
          type="button"
          onclick={() => (xdebugMenuOpen = !xdebugMenuOpen)}
          disabled={xdebugBusy}
          aria-haspopup="menu"
          aria-expanded={xdebugMenuOpen}
          title={m.system_php_xdebugModeTitle()}
          class="inline-flex items-center gap-1 px-3 py-1.5 rounded-r-lg border border-gray-200 dark:border-lerd-border transition-colors text-xs font-medium text-gray-700 dark:text-gray-200 bg-emerald-50/60 dark:bg-emerald-900/15 hover:bg-emerald-50 dark:hover:bg-emerald-900/25 disabled:opacity-50"
        >
          <span class="font-mono">{xdebugMode}</span>
          <svg class="w-3 h-3 transition-transform {xdebugMenuOpen ? 'rotate-180' : ''}" fill="none" stroke="currentColor" stroke-width="2.5" viewBox="0 0 24 24" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="6 9 12 15 18 9"/>
          </svg>
        </button>
        {#if xdebugMenuOpen}
          <div
            role="menu"
            class="absolute right-0 top-full mt-1 z-50 min-w-40 rounded-xl bg-white dark:bg-lerd-card border border-gray-200 dark:border-lerd-border shadow-xl py-1"
          >
            {#each XDEBUG_MODES as mode (mode)}
              {@const selected = mode === xdebugMode}
              <button
                type="button"
                role="menuitem"
                onclick={() => {
                  xdebugMenuOpen = false;
                  onSetXdebugMode({ target: { value: mode } } as unknown as Event);
                }}
                class="w-full text-left px-3 py-1.5 text-xs font-mono hover:bg-gray-50 dark:hover:bg-white/5 transition-colors {selected ? 'text-lerd-red font-semibold' : 'text-gray-700 dark:text-gray-200'}"
              >
                {mode}
              </button>
            {/each}
          </div>
        {/if}
      {/if}
    </div>
    <ButtonMenu actions={headerActions} busy={headerBusy} />
  </div>
</div>

{#if removeError}
  <div class="mx-3 sm:mx-5 my-2 text-xs font-medium text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-500/10 rounded-lg px-3 py-1.5 shrink-0">{removeError}</div>
{/if}

<DetailTabs {tabs} {active} onchange={(id) => (active = id)} />
{#if active === 'logs' && running}
  <LogViewer path={'/api/logs/' + container} />
{:else if active === 'sites'}
  <div class="px-3 sm:px-5 py-3 shrink-0">
    {#if sitesUsing.length === 0}
      <p class="text-sm text-gray-400">{m.system_noSitesUsingPhp({ version })}</p>
    {:else}
      <div class="flex flex-wrap gap-2">
        {#each sitesUsing as s (s.domain)}
          <button
            onclick={() => goToTab('sites', s.domain)}
            class="inline-flex items-center gap-1.5 text-xs font-medium bg-gray-100 dark:bg-white/5 hover:bg-gray-200 dark:hover:bg-white/10 border border-gray-200 dark:border-lerd-border text-gray-700 dark:text-gray-300 rounded-full px-2.5 py-1 transition-colors"
          >
            <span class="w-1.5 h-1.5 rounded-full shrink-0 {s.fpm_running ? 'bg-emerald-500' : 'bg-gray-400'}"></span>
            {s.domain}
          </button>
        {/each}
      </div>
    {/if}
  </div>
{:else if active === 'config'}
  <PhpIniTab {version} />
{/if}
