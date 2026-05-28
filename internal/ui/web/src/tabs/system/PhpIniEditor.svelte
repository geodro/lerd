<script lang="ts">
  import { getPhpIni, savePhpIni } from '$stores/phpVersions';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    version: string;
  }
  let { version }: Props = $props();

  let open = $state(false);
  let loaded = $state(false);
  let loading = $state(false);
  let saving = $state(false);
  let saved = $state(false);
  let error = $state('');
  let content = $state('');
  let path = $state('');
  let savedTimer: ReturnType<typeof setTimeout> | null = null;

  function clearSavedTimer() {
    if (savedTimer !== null) {
      clearTimeout(savedTimer);
      savedTimer = null;
    }
  }

  // Reset when the selected version changes so a stale editor body never
  // leaks across versions. Clear the saved flag and its pending timer too,
  // otherwise switching versions inside the 2.5s confirmation window leaves
  // the new version showing a stale "Saved" until the old timer fires.
  $effect(() => {
    version;
    open = false;
    loaded = false;
    content = '';
    error = '';
    saved = false;
    clearSavedTimer();
  });

  async function load() {
    loading = true;
    error = '';
    saved = false;
    try {
      const ini = await getPhpIni(version);
      content = ini.content;
      path = ini.path;
      loaded = true;
    } catch (e) {
      error = e instanceof Error ? e.message : 'failed';
    } finally {
      loading = false;
    }
  }

  async function toggle() {
    open = !open;
    if (open && !loaded && !loading) await load();
  }

  async function save() {
    saving = true;
    error = '';
    saved = false;
    clearSavedTimer();
    try {
      await savePhpIni(version, content);
      saved = true;
      savedTimer = setTimeout(() => {
        saved = false;
        savedTimer = null;
      }, 2500);
    } catch (e) {
      error = e instanceof Error && e.message ? e.message : m.system_php_iniSaveError();
    } finally {
      saving = false;
    }
  }
</script>

<div>
  <button
    onclick={toggle}
    class="flex items-center justify-between w-full text-left group"
    aria-expanded={open}
  >
    <div>
      <p class="text-sm font-medium text-gray-700 dark:text-gray-300">{m.system_php_ini()}</p>
      <p class="text-xs text-gray-400 mt-0.5">{m.system_php_iniHint()}</p>
    </div>
    <span class="text-gray-400 group-hover:text-gray-600 dark:group-hover:text-gray-300 text-xs">
      {open ? '▾' : '▸'}
    </span>
  </button>

  {#if open}
    <div class="mt-3 space-y-2">
      {#if loading}
        <p class="text-xs text-gray-400">…</p>
      {:else}
        <textarea
          bind:value={content}
          spellcheck="false"
          autocomplete="off"
          autocapitalize="off"
          class="w-full h-56 font-mono text-[11px] leading-relaxed rounded-lg border border-gray-200 dark:border-lerd-border bg-gray-50 dark:bg-black/40 text-gray-700 dark:text-gray-300 p-3 resize-y focus:outline-none focus:ring-1 focus:ring-sky-500"
        ></textarea>
        {#if path}
          <p class="text-[10px] text-gray-400 dark:text-gray-600 font-mono break-all">{path}</p>
        {/if}
        <div class="flex items-center gap-3">
          <button
            onclick={save}
            disabled={saving}
            class="px-3 py-1.5 rounded-md text-xs font-semibold bg-sky-600 hover:bg-sky-500 text-white disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
          >
            {saving ? m.system_php_iniSaving() : m.system_php_iniSave()}
          </button>
          {#if saved}
            <span class="text-xs text-green-600 dark:text-green-400">{m.system_php_iniSaved()}</span>
          {/if}
          {#if error}
            <span class="text-xs text-red-500">{error}</span>
          {/if}
        </div>
      {/if}
    </div>
  {/if}
</div>
