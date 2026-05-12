<script lang="ts">
  import { status, loadStatus } from '$stores/status';
  import { nodeVersions, loadNodeVersions, setDefaultNode, removeNode, installNode } from '$stores/nodeVersions';
  import { sites, sitesByNode, openSiteInBrowser } from '$stores/sites';
  import { goToTab } from '$stores/route';
  import DetailButton from '$components/DetailButton.svelte';
  import { m } from '../../paraglide/messages.js';

  // Mirror the store value into local $state via subscribe so the
  // reactivity is direct (bypasses any auto-subscription edge case).
  let nodeDefault = $state('');
  $effect(() => status.subscribe((s) => { nodeDefault = s.node_default || ''; }));

  let defaultBusy = $state<string | null>(null);
  let saveError = $state('');
  let removeBusy = $state<string | null>(null);
  let removeError = $state<Record<string, string>>({});

  let newVersion = $state('');
  let installBusy = $state(false);
  let installDone = $state(false);
  let installError = $state('');

  // staged holds a pending pick the user has clicked but not saved. null
  // means no staged change (radio reflects nodeDefault). After a successful
  // save we reset to null explicitly so the Save button disables.
  let staged = $state<string | null>(null);
  const selectedDefault = $derived(staged ?? nodeDefault);
  const dirty = $derived(staged !== null && staged !== nodeDefault);

  async function onSaveDefault() {
    if (!dirty || staged === null) return;
    const target = staged;
    defaultBusy = target;
    saveError = '';
    try {
      const ok = await setDefaultNode(target);
      await loadStatus();
      await loadNodeVersions();
      if (ok) {
        staged = null;
      } else {
        saveError = m.common_failed();
      }
    } catch (e) {
      saveError = e instanceof Error ? e.message : m.common_failed();
    } finally {
      defaultBusy = null;
    }
  }

  async function onRemove(v: string) {
    removeBusy = v;
    removeError = { ...removeError, [v]: '' };
    try {
      const ok = await removeNode(v);
      if (!ok) removeError = { ...removeError, [v]: m.common_failed() };
      await loadStatus();
      await loadNodeVersions();
    } finally {
      removeBusy = null;
    }
  }

  async function onInstall() {
    if (!newVersion.trim()) return;
    installBusy = true;
    installError = '';
    installDone = false;
    try {
      const ok = await installNode(newVersion.trim());
      if (ok) {
        installDone = true;
        newVersion = '';
        setTimeout(() => (installDone = false), 2000);
      } else {
        installError = m.system_node_installFailed();
      }
    } catch (e) {
      installError = e instanceof Error ? e.message : m.common_failed();
    } finally {
      installBusy = false;
    }
  }

  function sitesForVersion(v: string) {
    return $sites.filter((s) => s.node_version === v || (!s.node_version && v === nodeDefault));
  }
</script>

<div class="flex-1 overflow-y-auto">
  <div class="flex flex-wrap items-center justify-between gap-y-2 px-3 sm:px-5 py-4 border-b border-gray-100 dark:border-lerd-border">
    <div class="flex items-center gap-3">
      <span class="font-semibold text-gray-900 dark:text-white text-base">{m.system_nodeJs()}</span>
      {#if !$status.node_managed_by_lerd}
        <span class="text-[10px] font-medium text-blue-500 dark:text-blue-400 bg-blue-50 dark:bg-blue-500/10 rounded-sm px-1.5 py-0.5">{m.system_system()}</span>
      {/if}
    </div>
  </div>

  <div class="px-3 sm:px-5 py-4 space-y-3">
    {#if !$status.node_managed_by_lerd}
      <div class="text-sm text-blue-700 dark:text-blue-300 bg-blue-50 dark:bg-blue-500/10 border border-blue-200 dark:border-blue-500/20 rounded-lg px-3 py-2.5">
        <span class="font-medium">{m.system_node_managedBySystem()}</span> {m.system_node_managedBySystemHint()}
      </div>
    {/if}

    {#if $nodeVersions.length === 0}
      <p class="text-sm text-gray-400">{m.system_node_noneInstalled()}</p>
    {:else}
      {#if $nodeVersions.length > 1}
        <div class="flex items-center justify-end gap-2">
          {#if saveError}<span class="text-xs text-red-500">{saveError}</span>{/if}
          <DetailButton
            tone="primary"
            onclick={onSaveDefault}
            disabled={!dirty || defaultBusy !== null}
            loading={defaultBusy !== null}
          >{m.common_save()}</DetailButton>
        </div>
      {/if}
      <div class="space-y-2">
        {#each $nodeVersions as v (v)}
          {@const siteList = sitesForVersion(v)}
          {@const siteCount = $sitesByNode.get(v) ?? 0}
          {@const isDefault = v === nodeDefault}
          {@const isSelected = v === selectedDefault}
          {@const canRemove = siteCount === 0 && $status.node_managed_by_lerd && !isDefault}
          <div class="border {isSelected && !isDefault ? 'border-lerd-red/60 dark:border-lerd-red/60' : 'border-gray-200 dark:border-lerd-border'} rounded-lg p-3 bg-white dark:bg-lerd-card transition-colors">
            <div class="flex items-center gap-3 flex-wrap">
              <button
                type="button"
                onclick={() => (staged = v === nodeDefault ? null : v)}
                disabled={defaultBusy !== null}
                aria-pressed={isSelected}
                aria-label={'Node ' + v}
                class="flex items-center gap-2 cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
              >
                <span
                  class="relative w-4 h-4 rounded-full border-2 shrink-0 transition-colors
                    {isSelected
                      ? 'border-lerd-red'
                      : 'border-gray-400 dark:border-gray-500 hover:border-gray-500 dark:hover:border-gray-400'}"
                >
                  {#if isSelected}
                    <span class="absolute inset-[2px] rounded-full bg-lerd-red"></span>
                  {/if}
                </span>
                <span class="text-sm font-semibold text-gray-900 dark:text-white">Node {v}</span>
              </button>
              {#if isDefault}
                <span class="text-[10px] font-medium text-lerd-red bg-red-50 dark:bg-red-900/20 px-1.5 py-0.5 rounded-sm">{m.common_default()}</span>
              {/if}
              <span class="text-xs text-gray-400 dark:text-gray-500">
                {siteCount} {siteCount === 1 ? m.common_site() : m.common_sites()}
              </span>
              <div class="ml-auto">
                <DetailButton
                  tone="danger"
                  onclick={() => onRemove(v)}
                  disabled={!canRemove || removeBusy === v}
                  loading={removeBusy === v}
                  title={!$status.node_managed_by_lerd
                    ? m.system_node_cannotRemoveSystem()
                    : isDefault
                      ? m.system_node_cannotRemoveDefault()
                      : siteCount > 0
                        ? m.system_node_cannotRemove()
                        : m.system_node_removeTitle()}
                >{m.common_remove()}</DetailButton>
              </div>
            </div>
            {#if siteList.length > 0}
              <div class="flex flex-wrap gap-1.5 mt-3">
                {#each siteList as s (s.domain)}
                  <a
                    href={(s.tls ? 'https://' : 'http://') + s.domain}
                    onclick={(e) => {
                      e.preventDefault();
                      goToTab('sites', s.domain);
                    }}
                    ondblclick={(e) => {
                      e.preventDefault();
                      openSiteInBrowser(s);
                    }}
                    class="inline-flex items-center gap-1.5 text-xs font-medium bg-gray-100 dark:bg-white/5 hover:bg-gray-200 dark:hover:bg-white/10 border border-gray-200 dark:border-lerd-border text-gray-700 dark:text-gray-300 rounded-full px-2.5 py-1 transition-colors"
                  >
                    <span class="w-1.5 h-1.5 rounded-full shrink-0 bg-gray-400"></span>
                    {s.domain}
                  </a>
                {/each}
              </div>
            {/if}
            {#if removeError[v]}
              <p class="text-xs text-red-500 mt-2">{removeError[v]}</p>
            {/if}
          </div>
        {/each}
      </div>
    {/if}

    <div class="border border-dashed border-gray-200 dark:border-lerd-border rounded-lg p-3 bg-gray-50/50 dark:bg-white/2">
      <p class="text-xs font-semibold text-gray-700 dark:text-gray-300 mb-2">{m.system_node_installNewTitle()}</p>
      <p class="text-xs text-gray-400 mb-2">
        {@html m.system_node_installNewHint({ major: '<code class="font-mono bg-gray-100 dark:bg-white/5 px-1 rounded-sm">22</code>', specific: '<code class="font-mono bg-gray-100 dark:bg-white/5 px-1 rounded-sm">22.12.0</code>' })}
      </p>
      <div class="flex items-center gap-2">
        <input
          type="text"
          bind:value={newVersion}
          onkeydown={(e) => e.key === 'Enter' && onInstall()}
          placeholder={m.system_node_installPlaceholder()}
          disabled={installBusy || !$status.node_managed_by_lerd}
          title={!$status.node_managed_by_lerd ? m.system_node_managedBySystem() : ''}
          class="text-sm bg-white dark:bg-lerd-card border border-gray-200 dark:border-lerd-border rounded-lg px-3 py-1.5 w-28 text-gray-700 dark:text-gray-200 placeholder-gray-400 dark:placeholder-gray-600 focus:outline-hidden focus:border-lerd-red/50 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
        />
        <DetailButton
          tone="primary"
          onclick={onInstall}
          disabled={installBusy || !newVersion.trim() || !$status.node_managed_by_lerd}
          loading={installBusy}
        >{m.common_install()}</DetailButton>
        {#if installDone}<span class="text-xs text-emerald-600 dark:text-emerald-500">{m.system_node_installed()}</span>{/if}
        {#if installError}<span class="text-xs text-red-500">{installError}</span>{/if}
      </div>
    </div>
  </div>
</div>
