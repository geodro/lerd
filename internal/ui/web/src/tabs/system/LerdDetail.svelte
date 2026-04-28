<script lang="ts">
  import { onMount } from 'svelte';
  import { version, loadVersion } from '$stores/version';
  import { accessMode } from '$stores/accessMode';
  import { lan, loadLANStatus, toggleLAN, generateRemoteSetupCode, copySetupCurl } from '$stores/lan';
  import {
    remoteControl,
    loadRemoteControl,
    disableRemoteControl
  } from '$stores/remoteControl';
  import { openRemoteControlModal, openLANProgressModal, type LANAction } from '$stores/modals';
  import { autostartEnabled, loadAutostart, toggleAutostart } from '$stores/autostart';
  import Toggle from '$components/Toggle.svelte';
  import LanguageSwitcher from '$components/LanguageSwitcher.svelte';
  import { apiFetch } from '$lib/api';
  import { m } from '../../paraglide/messages.js';

  onMount(() => {
    loadLANStatus();
    loadRemoteControl();
    loadAutostart();
  });

  function startLAN(action: LANAction) {
    openLANProgressModal(action);
    toggleLAN(action);
  }

  let autostartBusy = $state(false);
  async function onToggleAutostart() {
    autostartBusy = true;
    try {
      await toggleAutostart(!$autostartEnabled);
    } finally {
      autostartBusy = false;
    }
  }

  let updateTerminalLoading = $state(false);
  let updateTerminalError = $state('');
  async function openUpdateTerminal() {
    updateTerminalLoading = true;
    updateTerminalError = '';
    try {
      const res = await apiFetch('/api/lerd/update-terminal', { method: 'POST' });
      const data = (await res.json()) as { ok?: boolean; error?: string };
      if (!data.ok) updateTerminalError = data.error || m.common_failed();
    } catch (e) {
      updateTerminalError = e instanceof Error ? e.message : m.common_failed();
    } finally {
      updateTerminalLoading = false;
    }
  }

  async function doDisableRemoteControl() {
    await disableRemoteControl();
  }
</script>

<div class="flex-1 overflow-y-auto">
  <div class="flex flex-wrap items-center justify-between gap-y-2 px-3 sm:px-5 py-4 border-b border-gray-100 dark:border-lerd-border">
    <span class="font-semibold text-gray-900 dark:text-white text-base">{m.system_lerd()}</span>
    <span class="inline-flex items-center gap-1.5 text-xs font-medium px-2.5 py-1 rounded-full bg-gray-100 dark:bg-white/5 text-gray-600 dark:text-gray-400 font-mono">v{$version.current}</span>
  </div>

  <div class="px-3 sm:px-5 py-4 space-y-4">
    {#if $version.checked && !$version.hasUpdate}
      <div class="flex items-center gap-2 text-sm text-emerald-600 dark:text-emerald-500">
        <svg class="w-4 h-4 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M5 13l4 4L19 7"/>
        </svg>
        {m.system_lerd_latest()}
      </div>
    {/if}

    {#if $version.hasUpdate}
      <div class="space-y-3">
        <div class="flex flex-wrap items-center gap-3">
          <span class="inline-flex items-center gap-1.5 text-sm font-medium text-yellow-700 dark:text-yellow-400">
            <svg class="w-4 h-4 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 16V4m0 0L3 8m4-4l4 4m6 0v12m0 0l4-4m-4 4l-4-4"/>
            </svg>
            {m.system_lerd_available({ version: $version.latest })}
          </span>
        </div>
        <p class="text-xs text-gray-500 dark:text-gray-400">
          {@html m.system_lerd_updateHint({ cmd: '<code class="bg-gray-100 dark:bg-white/10 px-1.5 py-0.5 rounded font-mono">lerd update</code>' })}
        </p>
        {#if $accessMode.loopback}
          <div class="flex items-center gap-2">
            <button
              onclick={openUpdateTerminal}
              disabled={updateTerminalLoading}
              class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium bg-gray-100 hover:bg-gray-200 dark:bg-white/5 dark:hover:bg-white/10 text-gray-700 dark:text-gray-300 disabled:opacity-50 transition-colors"
            >
              {#if updateTerminalLoading}
                <svg class="w-3.5 h-3.5 animate-spin" fill="none" viewBox="0 0 24 24">
                  <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"/>
                  <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"/>
                </svg>
                {m.system_lerd_openingTerminal()}
              {:else}
                <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z"/>
                </svg>
                {m.system_lerd_openTerminal()}
              {/if}
            </button>
          </div>
        {/if}
        {#if updateTerminalError}
          <p class="text-xs text-red-500">{updateTerminalError}</p>
        {/if}
        {#if $version.changelog}
          <div>
            <p class="text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wide mb-2">{m.system_lerd_whatsNew()}</p>
            <pre class="text-xs text-gray-600 dark:text-gray-400 bg-gray-50 dark:bg-white/[0.03] rounded-lg p-3 overflow-x-auto whitespace-pre-wrap font-mono leading-relaxed border border-gray-100 dark:border-lerd-border">{$version.changelog}</pre>
          </div>
        {/if}
      </div>
    {/if}

    <button
      onclick={loadVersion}
      disabled={$version.checking}
      class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium bg-gray-100 hover:bg-gray-200 dark:bg-white/5 dark:hover:bg-white/10 disabled:opacity-40 text-gray-700 dark:text-gray-300 transition-colors"
    >
      {#if $version.checking}
        <svg class="w-3.5 h-3.5 animate-spin" fill="none" viewBox="0 0 24 24">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"/>
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"/>
        </svg>
        {m.system_lerd_checking()}
      {:else}
        <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"/>
        </svg>
        {m.system_lerd_checkForUpdates()}
      {/if}
    </button>

    <div class="flex items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
      <svg class="w-3.5 h-3.5 shrink-0 text-yellow-500" fill="currentColor" viewBox="0 0 20 20">
        <path d="M9.049 2.927c.3-.921 1.603-.921 1.902 0l1.286 3.957a1 1 0 00.95.69h4.162c.969 0 1.371 1.24.588 1.81l-3.367 2.446a1 1 0 00-.364 1.118l1.287 3.957c.3.922-.755 1.688-1.54 1.118l-3.366-2.446a1 1 0 00-1.176 0l-3.366 2.446c-.784.57-1.838-.196-1.54-1.118l1.287-3.957a1 1 0 00-.364-1.118L2.098 9.384c-.783-.57-.38-1.81.588-1.81h4.162a1 1 0 00.95-.69l1.286-3.957z"/>
      </svg>
      <span>{m.system_lerd_starBlurb()}</span>
      <a href="https://github.com/geodro/lerd" target="_blank" rel="noopener" class="font-medium text-lerd-red hover:text-lerd-redhov underline-offset-2 hover:underline">{m.system_lerd_starCta()}</a>
      <span>{m.system_lerd_starAfter()}</span>
    </div>

    <div class="border-t border-gray-100 dark:border-lerd-border pt-4">
      <div class="flex items-center justify-between mb-2">
        <span class="text-sm font-semibold text-gray-700 dark:text-gray-300">{m.system_language_title()}</span>
      </div>
      <div class="flex items-center justify-between gap-4">
        <p class="text-xs text-gray-500 dark:text-gray-400">{m.system_language_description()}</p>
        <LanguageSwitcher />
      </div>
    </div>

    <div class="border-t border-gray-100 dark:border-lerd-border pt-4">
      <div class="flex items-center justify-between mb-2">
        <span class="text-sm font-semibold text-gray-700 dark:text-gray-300">{m.system_autostart_title()}</span>
        <span class="inline-flex items-center gap-1.5 text-[10px] font-medium px-2 py-0.5 rounded-full {$autostartEnabled ? 'bg-emerald-100 dark:bg-emerald-500/15 text-emerald-700 dark:text-emerald-400' : 'bg-gray-100 dark:bg-white/5 text-gray-500 dark:text-gray-400'}">
          <span class="w-1.5 h-1.5 rounded-full {$autostartEnabled ? 'bg-emerald-500' : 'bg-gray-400'}"></span>
          {$autostartEnabled ? m.system_autostart_enabled() : m.system_autostart_disabled()}
        </span>
      </div>
      <div class="flex items-center justify-between gap-4">
        <p class="text-xs text-gray-500 dark:text-gray-400">{m.system_autostart_description()}</p>
        <Toggle
          on={$autostartEnabled}
          loading={autostartBusy}
          onclick={onToggleAutostart}
          title={$autostartEnabled ? 'Disable autostart' : 'Enable autostart'}
        />
      </div>
    </div>

    <div class="border-t border-gray-100 dark:border-lerd-border pt-4">
      <div class="flex items-center justify-between mb-2">
        <span class="text-sm font-semibold text-gray-700 dark:text-gray-300">{m.system_lan_title()}</span>
        <span class="inline-flex items-center gap-1.5 text-[10px] font-medium px-2 py-0.5 rounded-full {$lan.exposed ? 'bg-emerald-100 dark:bg-emerald-500/15 text-emerald-700 dark:text-emerald-400' : 'bg-gray-100 dark:bg-white/5 text-gray-500 dark:text-gray-400'}">
          <span class="w-1.5 h-1.5 rounded-full {$lan.exposed ? 'bg-emerald-500' : 'bg-gray-400'}"></span>
          {$lan.exposed ? m.system_lan_exposed() : m.system_lan_loopback()}
        </span>
      </div>
      <p class="text-xs text-gray-500 dark:text-gray-400 mb-3">
        {#if $lan.exposed}
          {@html m.system_lan_exposedDescription({
            ip: '<code class="bg-gray-100 dark:bg-white/10 px-1.5 py-0.5 rounded font-mono">' + $lan.lanIP + '</code>',
            pattern: '<code class="bg-gray-100 dark:bg-white/10 px-1.5 py-0.5 rounded font-mono">*.test</code>',
            loop4: '<code class="bg-gray-100 dark:bg-white/10 px-1.5 py-0.5 rounded font-mono">127.0.0.1</code>',
            loop6: '<code class="bg-gray-100 dark:bg-white/10 px-1.5 py-0.5 rounded font-mono">::1</code>'
          })}
        {:else}
          {@html m.system_lan_loopbackDescription({
            loop4: '<code class="bg-gray-100 dark:bg-white/10 px-1.5 py-0.5 rounded font-mono">127.0.0.1</code>',
            loop6: '<code class="bg-gray-100 dark:bg-white/10 px-1.5 py-0.5 rounded font-mono">::1</code>'
          })}
        {/if}
      </p>

      {#if $lan.macos}
        <p class="text-xs text-amber-600 dark:text-amber-400 bg-amber-50 dark:bg-amber-500/10 border border-amber-200 dark:border-amber-500/30 rounded-lg px-3 py-2 mb-3">
          {@html m.system_lan_macosWarning({ pattern: '<code class="font-mono">*.test</code>' })}
        </p>
      {/if}

      {#if $accessMode.loopback}
        <div class="flex items-center gap-2">
          {#if !$lan.exposed}
            <button
              onclick={() => startLAN('expose')}
              disabled={$lan.loading}
              class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium bg-gray-100 hover:bg-gray-200 dark:bg-white/5 dark:hover:bg-white/10 text-gray-700 dark:text-gray-300 disabled:opacity-50 transition-colors"
            >{m.system_lan_expose()}</button>
          {:else}
            <button
              onclick={() => startLAN('unexpose')}
              disabled={$lan.loading}
              class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium bg-gray-100 hover:bg-gray-200 dark:bg-white/5 dark:hover:bg-white/10 text-gray-700 dark:text-gray-300 disabled:opacity-50 transition-colors"
            >{m.system_lan_stop()}</button>
          {/if}
        </div>
      {/if}

      {#if $lan.error}<p class="text-xs text-red-500 mt-2">{$lan.error}</p>{/if}

      {#if $lan.exposed && $accessMode.loopback}
        <div class="mt-3 space-y-3">
          <div class="text-xs text-gray-600 dark:text-gray-400 bg-amber-50 dark:bg-amber-500/10 border border-amber-200 dark:border-amber-500/30 rounded-lg p-3 space-y-1">
            <p>{@html m.system_lan_postExpose_resolver({ addr: '<code class="bg-white/60 dark:bg-white/10 px-1.5 py-0.5 rounded font-mono">' + $lan.lanIP + ':5300</code>', unit: '<code class="bg-white/60 dark:bg-white/10 px-1.5 py-0.5 rounded font-mono">lerd-dns-forwarder.service</code>' })}</p>
            <p>{@html m.system_lan_postExpose_dnsmasq({ pattern: '<code class="bg-white/60 dark:bg-white/10 px-1.5 py-0.5 rounded font-mono">*.test</code>', ip: $lan.lanIP })}</p>
            <p><strong>{m.system_lan_postExpose_firewall()}</strong></p>
          </div>

          <div>
            <p class="text-xs font-semibold text-gray-700 dark:text-gray-300 mb-2">{m.system_lan_remote_title()}</p>
            <p class="text-xs text-gray-500 dark:text-gray-400 mb-2">{m.system_lan_remote_hint()}</p>

            {#if !$lan.setupCode}
              <button
                onclick={generateRemoteSetupCode}
                disabled={$lan.setupLoading}
                class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium bg-gray-100 hover:bg-gray-200 dark:bg-white/5 dark:hover:bg-white/10 text-gray-700 dark:text-gray-300 disabled:opacity-50 transition-colors"
              >
                {#if $lan.setupLoading}
                  <svg class="w-3.5 h-3.5 animate-spin" fill="none" viewBox="0 0 24 24">
                    <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"/>
                    <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"/>
                  </svg>
                  {m.system_lan_remote_generating()}
                {:else}
                  {m.system_lan_remote_generate()}
                {/if}
              </button>
            {:else}
              <div class="space-y-2">
                <div class="flex items-center justify-between gap-3 text-xs text-gray-600 dark:text-gray-400">
                  <span>{@html m.system_lan_remote_codeLabel({ code: '<code class="bg-gray-100 dark:bg-white/10 px-1.5 py-0.5 rounded font-mono text-sm">' + $lan.setupCode + '</code>' })}</span>
                  {#if $lan.setupExpiresIn}<span>{m.system_lan_remote_expiresIn({ time: $lan.setupExpiresIn })}</span>{/if}
                </div>
                <p class="text-xs text-gray-500 dark:text-gray-400">{m.system_lan_remote_runOnMachine()}</p>
                <ul class="text-[11px] text-gray-500 dark:text-gray-400 list-disc pl-4 space-y-0.5">
                  <li>{@html m.system_lan_remote_bullet1({ mkcert: '<code class="font-mono">mkcert</code>' })}</li>
                  <li>{@html m.system_lan_remote_bullet2({ resolver: '<code class="font-mono">/etc/resolver</code>', test: '<code class="font-mono">.test</code>' })}</li>
                  <li>{m.system_lan_remote_bullet3()}</li>
                </ul>
                <div class="relative">
                  <pre class="text-xs text-gray-700 dark:text-gray-300 bg-gray-50 dark:bg-white/[0.03] border border-gray-100 dark:border-lerd-border rounded-lg p-3 pr-12 overflow-x-auto font-mono whitespace-pre">{$lan.setupCurl}</pre>
                  <button
                    onclick={copySetupCurl}
                    title={$lan.setupCopied ? m.system_lan_remote_copyTooltip_copied() : m.system_lan_remote_copyTooltip_copy()}
                    class="absolute top-2 right-2 inline-flex items-center justify-center w-7 h-7 rounded text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-white/10 transition-colors"
                  >
                    {#if $lan.setupCopied}
                      <svg class="w-4 h-4 text-emerald-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"/>
                      </svg>
                    {:else}
                      <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"/>
                      </svg>
                    {/if}
                  </button>
                </div>
                <p class="text-[11px] text-gray-500 dark:text-gray-400">
                  {@html m.system_lan_remote_footer({ generate: '<em>' + m.system_lan_remote_generate() + '</em>' })}
                </p>
                <button onclick={generateRemoteSetupCode} disabled={$lan.setupLoading} class="text-xs text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 underline">{m.system_lan_remote_newCode()}</button>
              </div>
            {/if}
            {#if $lan.setupError}<p class="text-xs text-red-500 mt-2">{$lan.setupError}</p>{/if}
          </div>
        </div>
      {/if}
    </div>

    <div class="border-t border-gray-100 dark:border-lerd-border pt-4">
      <div class="flex items-center justify-between mb-2">
        <span class="text-sm font-semibold text-gray-700 dark:text-gray-300">{m.system_remote_title()}</span>
        <span
          class="inline-flex items-center gap-1.5 text-[10px] font-medium px-2 py-0.5 rounded-full {$remoteControl.enabled && $lan.exposed
            ? 'bg-emerald-100 dark:bg-emerald-500/15 text-emerald-700 dark:text-emerald-400'
            : $remoteControl.enabled && !$lan.exposed
              ? 'bg-amber-100 dark:bg-amber-500/15 text-amber-700 dark:text-amber-400'
              : 'bg-gray-100 dark:bg-white/5 text-gray-500 dark:text-gray-400'}"
        >
          <span class="w-1.5 h-1.5 rounded-full {$remoteControl.enabled && $lan.exposed
            ? 'bg-emerald-500'
            : $remoteControl.enabled && !$lan.exposed
              ? 'bg-amber-500'
              : 'bg-gray-400'}"></span>
          {$remoteControl.enabled && $lan.exposed ? m.system_remote_status_active() : $remoteControl.enabled && !$lan.exposed ? m.system_remote_status_inert() : m.system_remote_status_disabled()}
        </span>
      </div>
      <p class="text-xs text-gray-500 dark:text-gray-400 mb-3">
        {@html m.system_remote_description({ loop4: '<code class="bg-gray-100 dark:bg-white/10 px-1.5 py-0.5 rounded font-mono">127.0.0.1</code>', loop6: '<code class="bg-gray-100 dark:bg-white/10 px-1.5 py-0.5 rounded font-mono">::1</code>' })}
      </p>

      {#if $remoteControl.enabled}
        <div class="space-y-2">
          <p class="text-xs text-gray-600 dark:text-gray-400">
            {@html m.system_remote_usernameRow({ username: '<code class="bg-gray-100 dark:bg-white/10 px-1.5 py-0.5 rounded font-mono">' + $remoteControl.username + '</code>' })}
          </p>
          {#if !$lan.exposed}
            <p class="text-xs text-amber-600 dark:text-amber-400">
              {@html m.system_remote_inertWarning({ cmd: '<code class="font-mono">lerd lan:expose</code>', btn: '<em>' + m.system_lan_expose() + '</em>' })}
            </p>
          {/if}
          <button
            onclick={doDisableRemoteControl}
            disabled={$remoteControl.loading}
            class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium bg-red-50 hover:bg-red-100 dark:bg-red-500/10 dark:hover:bg-red-500/20 text-red-700 dark:text-red-400 disabled:opacity-50 transition-colors"
          >{m.system_remote_disable()}</button>
        </div>
      {:else}
        <div>
          <button
            onclick={openRemoteControlModal}
            disabled={!$lan.exposed}
            title={$lan.exposed ? '' : m.system_remote_enableDisabledHint()}
            class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium bg-gray-100 hover:bg-gray-200 dark:bg-white/5 dark:hover:bg-white/10 text-gray-700 dark:text-gray-300 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          >{m.system_remote_enable()}</button>
          {#if !$lan.exposed}
            <p class="text-xs text-gray-400 dark:text-gray-500 mt-2">{m.system_remote_exposeFirst()}</p>
          {/if}
        </div>
      {/if}
      {#if $remoteControl.error}<p class="text-xs text-red-500 mt-2">{$remoteControl.error}</p>{/if}
    </div>
  </div>
</div>
