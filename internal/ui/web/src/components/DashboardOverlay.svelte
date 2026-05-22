<script lang="ts">
  import { dashboardOpen, closeDashboard } from '$stores/dashboard';
  import { dashboardIconSvg } from '$lib/dashboardIcons';
  import {
    profilerEnabled,
    loadProfilerStatus,
    setProfiler,
    clearProfilerData
  } from '$stores/profiler';
  import Icon from './Icon.svelte';
  import StatusDot from './StatusDot.svelte';
  import {
    isSpxReportView,
    setSpxConfigHidden,
    padSpxControlPanel,
    fetchSpxReportCount
  } from '$lib/spxControls';
  import { m } from '../paraglide/messages.js';

  let busy = $state(false);
  let clearing = $state(false);
  let iframeEl = $state<HTMLIFrameElement | null>(null);
  let entryHref = $state('');
  let canGoBack = $state(false);
  let isReportView = $state(false);
  // The SPX Configuration form is collapsed by default so the report list
  // gets the whole view; the header button restores it on demand.
  let configHidden = $state(true);

  const isProfiler = $derived($dashboardOpen?.name === 'profiler');

  const headerBtnClass =
    'text-xs rounded-sm border border-gray-200 dark:border-lerd-border px-2 py-1 ' +
    'text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 transition-colors';

  // Reset iframe-history tracking whenever a different dashboard opens.
  $effect(() => {
    $dashboardOpen;
    entryHref = '';
    canGoBack = false;
    isReportView = false;
    configHidden = true;
  });

  $effect(() => {
    if (isProfiler) void loadProfilerStatus();
  });

  // SPX never re-fetches its report list while the control panel page is open,
  // so new captures stay hidden. Poll and reload the iframe only once the
  // report count has actually grown.
  $effect(() => {
    if (!isProfiler || isReportView || !$profilerEnabled) return;
    let lastReportCount = -1;
    const id = setInterval(async () => {
      if (document.hidden) return;
      const count = await fetchSpxReportCount();
      if (count === null) return;
      if (lastReportCount >= 0 && count > lastReportCount) reloadIframe();
      lastReportCount = count;
    }, 4000);
    return () => clearInterval(id);
  });

  // iframeWindow returns the embedded window only when it is same-origin and
  // therefore drivable; cross-origin service dashboards return null.
  function iframeWindow(): Window | null {
    try {
      const w = iframeEl?.contentWindow ?? null;
      void w?.location.href; // throws for a cross-origin frame
      return w;
    } catch {
      return null;
    }
  }

  function onIframeLoad() {
    const w = iframeWindow();
    const href = w?.location.href ?? '';
    if (href === '' || !w) {
      canGoBack = false;
      isReportView = false;
      return;
    }
    if (entryHref === '') entryHref = href;
    canGoBack = href !== entryHref;
    isReportView = isSpxReportView(href);
    if (!isReportView && isProfiler) {
      padSpxControlPanel(w.document);
      setSpxConfigHidden(w.document, configHidden);
    }
  }

  function toggleConfig() {
    configHidden = !configHidden;
    const w = iframeWindow();
    if (w) setSpxConfigHidden(w.document, configHidden);
  }

  function goBack() {
    iframeWindow()?.history.back();
  }

  function reloadIframe() {
    const w = iframeWindow();
    if (w) {
      w.location.reload();
    } else if (iframeEl) {
      iframeEl.src = iframeEl.src; // cross-origin: reload to its entry URL
    }
  }

  async function toggleProfiler() {
    if (busy) return;
    busy = true;
    try {
      await setProfiler(!$profilerEnabled);
    } finally {
      busy = false;
    }
  }

  // clearData wipes every captured SPX report, then reloads the embedded UI
  // so its report list reflects the now-empty data directory.
  async function clearProfilerReports() {
    if (clearing) return;
    clearing = true;
    try {
      await clearProfilerData();
      reloadIframe();
    } finally {
      clearing = false;
    }
  }
</script>

{#if $dashboardOpen}
  {@const d = $dashboardOpen}
  {@const iframeSrc = d.dashboard + (d.extraPath ?? '')}
  <div class="fixed top-0 right-0 left-0 bottom-16 md:left-14 md:bottom-0 z-30 flex flex-col bg-white dark:bg-lerd-bg">
    <div class="flex items-center justify-between px-4 py-2 border-b border-gray-200 dark:border-lerd-border shrink-0">
      <div class="flex items-center gap-3 min-w-0">
        {#if isProfiler}
          <button
            onclick={goBack}
            disabled={!canGoBack}
            title={m.common_back()}
            aria-label={m.common_back()}
            class="text-gray-400 enabled:hover:text-gray-700 dark:enabled:hover:text-gray-200 disabled:opacity-30 disabled:cursor-not-allowed transition-colors shrink-0"
          >
            <Icon name="back" />
          </button>
        {/if}
        <svg class="w-5 h-5 text-gray-500 dark:text-gray-400 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          {@html dashboardIconSvg(d.name)}
        </svg>
        <span class="text-sm font-medium text-gray-900 dark:text-white truncate">{d.label || d.name}</span>
        <a
          href={iframeSrc}
          target="_blank"
          rel="noopener"
          class="font-mono text-[10px] text-sky-600 dark:text-sky-400 hover:underline truncate"
        >{d.dashboard}</a>
      </div>
      <div class="flex items-center gap-2 shrink-0">
        {#if isProfiler}
          {#if !isReportView}
            <button
              onclick={toggleConfig}
              title={configHidden ? m.profiler_config_show() : m.profiler_config_hide()}
              class={headerBtnClass}
            >
              {configHidden ? m.profiler_config_show() : m.profiler_config_hide()}
            </button>
          {/if}
          <button
            onclick={clearProfilerReports}
            disabled={clearing}
            title={m.profiler_clear_title()}
            class="{headerBtnClass} disabled:opacity-50"
          >
            {clearing ? m.profiler_clear_busy() : m.profiler_clear()}
          </button>
          <button
            onclick={toggleProfiler}
            disabled={busy}
            aria-pressed={$profilerEnabled}
            class="flex items-center gap-1.5 text-xs rounded-sm border px-2 py-1 transition-colors disabled:opacity-50 {$profilerEnabled
              ? 'border-emerald-500/40 bg-emerald-50 dark:bg-emerald-900/20 text-emerald-700 dark:text-emerald-300 hover:border-emerald-500'
              : 'border-gray-200 dark:border-lerd-border text-gray-500 hover:text-gray-700 dark:hover:text-gray-300'}"
          >
            {#if $profilerEnabled}
              <StatusDot color="emerald" size="xs" pulse />
            {/if}
            {busy ? m.profiler_busy() : $profilerEnabled ? m.profiler_disarm() : m.profiler_arm()}
          </button>
        {/if}
        <button
          onclick={reloadIframe}
          title={m.common_refresh()}
          aria-label={m.common_refresh()}
          class="text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
        >
          <Icon name="refresh" />
        </button>
        <a
          href={iframeSrc}
          target="_blank"
          rel="noopener"
          title={m.common_openInNewTab()}
          aria-label={m.common_openInNewTab()}
          class="text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
        ><Icon name="external" /></a>
        <button
          onclick={closeDashboard}
          title={m.common_close()}
          aria-label={m.common_closeDashboard()}
          class="text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
        >
          <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/>
          </svg>
        </button>
      </div>
    </div>
    <iframe
      bind:this={iframeEl}
      onload={onIframeLoad}
      src={iframeSrc}
      class="flex-1 w-full bg-white border-0"
      title={d.label || d.name}
    ></iframe>
  </div>
{/if}
