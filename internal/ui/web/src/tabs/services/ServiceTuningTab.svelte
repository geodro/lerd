<script lang="ts">
  import { getServiceConfig, saveServiceConfig, type Service } from '$stores/services';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    svc: Service;
  }
  let { svc }: Props = $props();

  let content = $state('');
  let target = $state('');
  let loading = $state(true);
  let saving = $state(false);
  let saved = $state(false);
  let error = $state('');
  let savedTimer: ReturnType<typeof setTimeout> | null = null;

  function clearSavedTimer() {
    if (savedTimer !== null) {
      clearTimeout(savedTimer);
      savedTimer = null;
    }
  }

  // Load the override whenever the selected service changes. The name guard
  // drops a stale response if the user switches services mid-fetch. Clear
  // the saved flag and its pending timer too — otherwise switching services
  // inside the 2.5s confirmation window leaves the new service showing a
  // stale "Saved" badge until the old timer fires.
  $effect(() => {
    const name = svc.name;
    loading = true;
    error = '';
    saved = false;
    clearSavedTimer();
    getServiceConfig(name)
      .then((cfg) => {
        if (name !== svc.name) return;
        content = cfg.content;
        target = cfg.target;
      })
      .catch((e) => {
        if (name !== svc.name) return;
        error = e instanceof Error ? e.message : 'failed';
      })
      .finally(() => {
        if (name === svc.name) loading = false;
      });
  });

  async function save() {
    saving = true;
    error = '';
    saved = false;
    clearSavedTimer();
    try {
      await saveServiceConfig(svc.name, content);
      saved = true;
      savedTimer = setTimeout(() => {
        saved = false;
        savedTimer = null;
      }, 2500);
    } catch (e) {
      error = e instanceof Error && e.message ? e.message : m.services_tuning_saveError();
    } finally {
      saving = false;
    }
  }
</script>

<div class="p-3 sm:p-5 space-y-3 overflow-y-auto flex flex-col">
  <p class="text-[11px] text-gray-400 dark:text-gray-500 leading-relaxed">{m.services_tuning_desc()}</p>

  {#if loading}
    <p class="text-xs text-gray-400">…</p>
  {:else}
    <textarea
      bind:value={content}
      spellcheck="false"
      autocomplete="off"
      autocapitalize="off"
      class="w-full h-64 font-mono text-[11px] leading-relaxed rounded-lg border border-gray-200 dark:border-lerd-border bg-gray-50 dark:bg-black/40 text-gray-700 dark:text-gray-300 p-3 resize-y focus:outline-none focus:ring-1 focus:ring-sky-500"
    ></textarea>

    {#if target}
      <p class="text-[10px] text-gray-400 dark:text-gray-600 font-mono break-all">
        {m.services_tuning_mountedAt({ path: target })}
      </p>
    {/if}

    <div class="flex items-center gap-3">
      <button
        onclick={save}
        disabled={saving}
        class="px-3 py-1.5 rounded-md text-xs font-semibold bg-sky-600 hover:bg-sky-500 text-white disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
      >
        {saving ? m.services_tuning_saving() : m.services_tuning_save()}
      </button>
      {#if saved}
        <span class="text-xs text-green-600 dark:text-green-400">{m.services_tuning_saved()}</span>
      {/if}
      {#if error}
        <span class="text-xs text-red-500">{error}</span>
      {/if}
    </div>
  {/if}
</div>
