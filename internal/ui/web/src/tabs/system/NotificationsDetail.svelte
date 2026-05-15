<script lang="ts">
  import { onMount } from 'svelte';
  import Toggle from '$components/Toggle.svelte';
  import {
    notifyPrefs,
    setNotifyPref,
    setNotifyMaster,
    enableNotifications,
    permissionState,
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

  async function loadDevices() {
    try {
      const r = await apiFetch('/api/push/devices');
      if (r.ok) devices = (await r.json()) as Device[];
    } catch {
      devices = [];
    }
  }

  async function forget(endpoint: string) {
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

<div class="flex-1 overflow-y-auto p-6 max-w-2xl">
  <h2 class="text-base font-semibold text-gray-900 dark:text-white mb-4">
    {m.notify_settings_title()}
  </h2>

  {#if $permissionState === 'default'}
    <div class="mb-6 rounded-md border border-sky-300 dark:border-sky-500/40 bg-sky-50 dark:bg-sky-900/20 p-3 text-xs text-sky-900 dark:text-sky-200">
      <p class="mb-2">{m.notify_banner_subtitle()}</p>
      <button
        onclick={() => enableNotifications()}
        class="inline-flex items-center text-xs font-medium bg-sky-600 hover:bg-sky-700 text-white rounded-sm px-3 py-1.5 transition-colors"
      >{m.notify_banner_enable()}</button>
    </div>
  {:else if $permissionState === 'denied'}
    <div class="mb-6 rounded-md border border-red-300 dark:border-red-500/40 bg-red-50 dark:bg-red-900/20 p-3 text-xs text-red-900 dark:text-red-200">
      Notifications are blocked in this browser. Re-enable them from the site permissions menu in your browser.
    </div>
  {/if}

  <div class="space-y-1">
    <div class="flex items-start justify-between gap-4 py-2.5 border-b border-gray-100 dark:border-lerd-border">
      <div class="flex-1 min-w-0">
        <p class="text-sm font-medium text-gray-900 dark:text-white">
          {m.notify_settings_master()}
        </p>
        <p class="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
          {m.notify_settings_master_hint()}
        </p>
      </div>
      <Toggle
        on={$notifyPrefs.enabled}
        onclick={() => setNotifyMaster(!$notifyPrefs.enabled)}
        tone="accent"
      />
    </div>

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
      disabled={testing}
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
