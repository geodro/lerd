<script lang="ts">
  import { getNginxConfig, saveNginxConfig } from '$stores/nginx';
  import { m } from '../../paraglide/messages.js';

  let open = $state(false);
  let loaded = $state(false);
  let loading = $state(false);
  let saving = $state(false);
  let saved = $state(false);
  let error = $state('');
  let content = $state('');
  let path = $state('');

  async function load() {
    loading = true;
    error = '';
    saved = false;
    try {
      const cfg = await getNginxConfig();
      content = cfg.content;
      path = cfg.path;
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
    const ok = await saveNginxConfig(content);
    saving = false;
    if (ok) {
      saved = true;
      setTimeout(() => (saved = false), 2500);
    } else {
      error = m.system_nginx_confSaveError();
    }
  }
</script>

<div class="px-3 sm:px-5 py-3 shrink-0 border-t border-gray-100 dark:border-lerd-border">
  <button onclick={toggle} class="flex items-center justify-between w-full text-left group" aria-expanded={open}>
    <div>
      <p class="text-sm font-medium text-gray-700 dark:text-gray-300">{m.system_nginx_conf()}</p>
      <p class="text-xs text-gray-400 mt-0.5">{m.system_nginx_confHint()}</p>
    </div>
    <span class="text-gray-400 group-hover:text-gray-600 dark:group-hover:text-gray-300 text-xs">{open ? '▾' : '▸'}</span>
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
            {saving ? m.system_nginx_confSaving() : m.system_nginx_confSave()}
          </button>
          {#if saved}
            <span class="text-xs text-green-600 dark:text-green-400">{m.system_nginx_confSaved()}</span>
          {/if}
          {#if error}
            <span class="text-xs text-red-500">{error}</span>
          {/if}
        </div>
      {/if}
    </div>
  {/if}
</div>
