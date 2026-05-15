<script lang="ts">
  import { onMount } from 'svelte';
  import Toggle from '$components/Toggle.svelte';
  import DetailPanel from '$components/DetailPanel.svelte';
  import DetailHeader from '$components/DetailHeader.svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import {
    notifyPrefs,
    setNotifyPref,
    setNotifyMaster,
    enableNotifications,
    forgetCurrentBrowser,
    permissionState,
    autoSubscribeDisabled,
    detectBrowserFamily,
    ALL_KINDS,
    type NotifyKind
  } from '$lib/notify';
  import { apiFetch } from '$lib/api';
  import { m } from '../../paraglide/messages.js';

  interface Device {
    endpoint: string;
    ua: string;
    added_at: number;
    enabled: boolean;
    enabled_kinds?: string[];
  }

  let devices = $state<Device[]>([]);
  let testing = $state(false);
  let testSent = $state(false);
  let recheckAttempted = $state(false);
  let toggling = $state(false);

  const browserFamily = $derived(
    typeof navigator !== 'undefined' ? detectBrowserFamily(navigator.userAgent) : 'other'
  );
  const pageOrigin = $derived(typeof location !== 'undefined' ? location.origin : '');
  const effectiveEnabled = $derived(
    $permissionState === 'granted' && !$autoSubscribeDisabled && $notifyPrefs.enabled
  );

  async function loadDevices() {
    try {
      const r = await apiFetch('/api/push/devices');
      if (r.ok) devices = (await r.json()) as Device[];
    } catch {
      devices = [];
    }
  }

  async function toggleMaster() {
    if (toggling) return;
    toggling = true;
    try {
      if ($permissionState === 'default') {
        await enableNotifications();
        await loadDevices();
        return;
      }
      if ($permissionState === 'granted' && $autoSubscribeDisabled) {
        await enableNotifications();
        await loadDevices();
        if (!$notifyPrefs.enabled) setNotifyMaster(true);
        return;
      }
      setNotifyMaster(!$notifyPrefs.enabled);
    } finally {
      toggling = false;
    }
  }

  async function recheckPermission() {
    recheckAttempted = true;
    await enableNotifications();
  }

  async function forget(endpoint: string) {
    // Browser-side unsubscribe first so we don't race against initNotify
    // re-registering the same endpoint between the server delete and the
    // flag flip. forgetCurrentBrowser is a no-op for foreign endpoints.
    await forgetCurrentBrowser(endpoint);
    await apiFetch('/api/push/unsubscribe', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ endpoint })
    });
    await loadDevices();
  }

  async function sendTest() {
    testing = true;
    try {
      await apiFetch('/api/push/test', { method: 'POST' });
      testSent = true;
      setTimeout(() => (testSent = false), 2000);
    } finally {
      testing = false;
    }
  }

  function uaShort(ua: string): string {
    if (!ua) return 'Unknown browser';
    const match = ua.match(/(Edg|OPR|Brave|Chrome|Firefox|Safari)\/(\d+)/);
    if (match) return match[1] + ' ' + match[2];
    return ua.slice(0, 32);
  }

  function whenShort(unix: number): string {
    if (!unix) return '';
    const d = new Date(unix * 1000);
    return d.toLocaleDateString() + ' ' + d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  }

  const kindLabel: Record<NotifyKind, string> = {
    mail: m.notify_settings_category_mail(),
    worker_failed: m.notify_settings_category_worker_failed(),
    op_done: m.notify_settings_category_op_done(),
    update_available: m.notify_settings_category_update_available(),
    dump: m.notify_settings_category_dump()
  };
  const kindHint: Record<NotifyKind, string> = {
    mail: m.notify_settings_category_mail_hint(),
    worker_failed: m.notify_settings_category_worker_failed_hint(),
    op_done: m.notify_settings_category_op_done_hint(),
    update_available: m.notify_settings_category_update_available_hint(),
    dump: m.notify_settings_category_dump_hint()
  };

  onMount(loadDevices);
</script>

{#snippet trailing()}
  <div class="flex items-center gap-2">
    {#if $permissionState === 'denied'}
      <StatusPill tone="error" label={m.common_disabled()} />
    {:else if effectiveEnabled}
      <StatusPill tone="ok" label={m.common_enabled()} />
    {:else}
      <StatusPill tone="muted" label={m.common_disabled()} />
    {/if}
    {#if $permissionState !== 'denied'}
      <DetailButton
        tone={effectiveEnabled ? 'secondary' : 'success'}
        disabled={toggling}
        loading={toggling}
        onclick={toggleMaster}
      >
        {effectiveEnabled ? m.common_disable() : m.common_enable()}
      </DetailButton>
    {/if}
  </div>
{/snippet}

<DetailPanel>
  <DetailHeader title={m.notify_settings_title()} {trailing} />

  {#if $permissionState === 'default'}
    <div class="px-3 sm:px-5 pt-4 shrink-0">
      <div class="rounded-md border border-sky-300 dark:border-sky-500/40 bg-sky-50 dark:bg-sky-900/20 p-3 text-xs text-sky-900 dark:text-sky-200">
        {m.notify_banner_subtitle()}
      </div>
    </div>
  {:else if $permissionState === 'denied'}
    <div class="px-3 sm:px-5 pt-4 shrink-0">
      <div class="rounded-md border border-red-300 dark:border-red-500/40 bg-red-50 dark:bg-red-900/20 p-3 text-xs text-red-900 dark:text-red-200">
        <p class="font-medium mb-1">{m.notify_settings_denied_title()}</p>
        <p class="mb-2">{m.notify_settings_denied_body()}</p>
        {#if pageOrigin}
          <p class="mb-2 font-mono text-[11px] break-all">
            {m.notify_settings_denied_origin({ origin: pageOrigin })}
          </p>
        {/if}
        <p class="mb-3">
          {#if browserFamily === 'chromium'}
            {m.notify_settings_denied_chromium()}
          {:else if browserFamily === 'firefox'}
            {m.notify_settings_denied_firefox()}
          {:else if browserFamily === 'safari'}
            {m.notify_settings_denied_safari()}
          {:else}
            {m.notify_settings_denied_generic()}
          {/if}
        </p>
        <button
          onclick={recheckPermission}
          class="inline-flex items-center text-xs font-medium bg-red-600 hover:bg-red-700 text-white rounded-sm px-3 py-1.5 transition-colors"
        >{m.notify_settings_denied_recheck()}</button>
        {#if recheckAttempted}
          <p class="mt-2 text-[11px]">{m.notify_settings_denied_still_blocked()}</p>
        {/if}
      </div>
    </div>
  {:else if $permissionState === 'granted' && $autoSubscribeDisabled}
    <div class="px-3 sm:px-5 pt-4 shrink-0">
      <div class="rounded-md border border-amber-300 dark:border-amber-500/40 bg-amber-50 dark:bg-amber-900/20 p-3 text-xs text-amber-900 dark:text-amber-200">
        <p class="font-medium mb-1">{m.notify_settings_unsubscribed_title()}</p>
        <p>{m.notify_settings_unsubscribed_body()}</p>
      </div>
    </div>
  {/if}

  <div class="flex-1 overflow-y-auto px-3 sm:px-5 py-4">
    <div class="space-y-1">
      {#each ALL_KINDS as kind (kind)}
        <div class="flex items-start justify-between gap-4 py-2.5 border-b border-gray-100 dark:border-lerd-border">
          <div class="flex-1 min-w-0">
            <p class="text-sm text-gray-900 dark:text-white">{kindLabel[kind]}</p>
            <p class="text-xs text-gray-500 dark:text-gray-400 mt-0.5">{kindHint[kind]}</p>
          </div>
          <Toggle
            on={$notifyPrefs.kinds[kind] && $notifyPrefs.enabled}
            disabled={!$notifyPrefs.enabled}
            onclick={() => setNotifyPref(kind, !$notifyPrefs.kinds[kind])}
            tone="accent"
          />
        </div>
      {/each}
    </div>

    <div class="mt-6">
      <button
        onclick={sendTest}
        disabled={testing || !effectiveEnabled}
        class="text-xs font-medium border border-gray-200 dark:border-lerd-border hover:border-gray-300 dark:hover:border-lerd-muted rounded-sm px-3 py-1.5 transition-colors disabled:opacity-50"
      >
        {testSent ? m.notify_settings_test_sent() : m.notify_settings_test()}
      </button>
    </div>

    <h3 class="text-sm font-medium text-gray-900 dark:text-white mt-8 mb-3">
      {m.notify_settings_devices_title()}
    </h3>

    {#if devices.length === 0}
      <p class="text-xs text-gray-500 dark:text-gray-400">
        {m.notify_settings_devices_none()}
      </p>
    {:else}
      <ul class="space-y-1">
        {#each devices as d (d.endpoint)}
          <li class="flex items-center justify-between gap-3 py-2 border-b border-gray-100 dark:border-lerd-border">
            <div class="min-w-0">
              <p class="text-xs text-gray-900 dark:text-white truncate">{uaShort(d.ua)}</p>
              <p class="text-[10px] text-gray-500 dark:text-gray-400">{whenShort(d.added_at)}</p>
            </div>
            <button
              onclick={() => forget(d.endpoint)}
              class="text-[11px] text-gray-500 hover:text-red-600 dark:text-gray-400 dark:hover:text-red-400 transition-colors"
            >{m.notify_settings_devices_forget()}</button>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</DetailPanel>
