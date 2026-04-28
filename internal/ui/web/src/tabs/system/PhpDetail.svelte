<script lang="ts">
  import DetailPanel from '$components/DetailPanel.svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import Toggle from '$components/Toggle.svelte';
  import InfoRow from '$components/InfoRow.svelte';
  import LogViewer from '$components/LogViewer.svelte';
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
</script>

<DetailPanel>
  <div
    class="flex flex-wrap items-center justify-between gap-y-2 px-3 sm:px-5 py-4 border-b border-gray-100 dark:border-lerd-border shrink-0"
  >
    <div class="flex items-center gap-3">
      <span class="font-semibold text-gray-900 dark:text-white text-base">PHP {version}</span>
      <StatusPill tone={running ? 'ok' : 'muted'} label={running ? m.common_running() : m.common_stopped()} />
      {#if siteCount > 0}
        <span class="text-xs text-gray-400 dark:text-gray-500">
          {siteCount} {siteCount === 1 ? m.common_site() : m.common_sites()}
        </span>
      {/if}
    </div>
    <div class="flex items-center gap-2">
      {#if !isDefault}
        <DetailButton onclick={onSetDefault} disabled={defaultBusy} loading={defaultBusy}>
          {m.system_php_setDefault()}
        </DetailButton>
      {/if}
      {#if !isDefault}
        {#if running}
          <DetailButton
            onclick={onToggleFpm}
            disabled={fpmBusy}
            loading={fpmBusy}
            title={siteCount > 0 ? m.system_php_stopWarn({ count: siteCount }) : m.system_php_stopTitle()}
          >{m.common_stop()}</DetailButton>
        {:else}
          <DetailButton
            tone="success"
            onclick={onToggleFpm}
            disabled={fpmBusy}
            loading={fpmBusy}
            title={m.system_php_startTitle()}
          >{m.common_start()}</DetailButton>
        {/if}
        <DetailButton
          tone="danger"
          onclick={onRemove}
          disabled={removeBusy}
          loading={removeBusy}
          title={siteCount > 0 ? m.system_php_removeWarn({ count: siteCount }) : m.system_php_removeTitle()}
        >{m.common_remove()}</DetailButton>
      {/if}
    </div>
  </div>

  <div class="px-3 sm:px-5 py-3 space-y-4 shrink-0">
    <div class="flex items-center justify-between">
      <div>
        <p class="text-sm font-medium text-gray-700 dark:text-gray-300">{m.system_php_xdebug()}</p>
        <p class="text-xs text-gray-400 mt-0.5">{m.system_php_xdebugHint()}</p>
      </div>
      <div class="flex items-center gap-2">
        {#if xdebugEnabled}
          <select
            value={xdebugMode}
            onchange={onSetXdebugMode}
            disabled={xdebugBusy}
            class="text-xs bg-white dark:bg-lerd-muted border border-gray-200 dark:border-lerd-border rounded px-1.5 py-0.5 text-gray-700 dark:text-gray-300 focus:outline-none focus:ring-1 focus:ring-purple-400 disabled:opacity-50"
            title="Xdebug mode"
          >
            {#each XDEBUG_MODES as mode (mode)}<option value={mode}>{mode}</option>{/each}
          </select>
        {/if}
        <Toggle
          on={xdebugEnabled}
          tone="violet"
          loading={xdebugBusy}
          onclick={onToggleXdebug}
          title={xdebugEnabled ? 'Disable Xdebug' : 'Enable Xdebug'}
        />
      </div>
    </div>

    <InfoRow label={m.system_container()} value={container} />

    <div>
      <p class="text-xs font-semibold text-gray-400 uppercase tracking-wider mb-2">{m.system_php_sites()}</p>
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

    {#if removeError}
      <div class="text-xs font-medium text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-500/10 rounded-lg px-3 py-1.5">{removeError}</div>
    {/if}
  </div>

  {#if running}
    <LogViewer path={'/api/logs/' + container} />
  {/if}
</DetailPanel>
