<script lang="ts">
  import { getSiteNginx, saveSiteNginx, type Site } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  let content = $state('');
  let path = $state('');
  let loading = $state(true);
  let saving = $state(false);
  let saved = $state(false);
  let error = $state('');

  // Load whenever the selected site changes; the domain guard drops a stale
  // response if the user switches sites mid-fetch.
  $effect(() => {
    const domain = site.domain;
    loading = true;
    error = '';
    saved = false;
    getSiteNginx(domain)
      .then((res) => {
        if (domain !== site.domain) return;
        content = res.content;
        path = res.path;
      })
      .catch((e) => {
        if (domain !== site.domain) return;
        error = e instanceof Error ? e.message : 'failed';
      })
      .finally(() => {
        if (domain === site.domain) loading = false;
      });
  });

  async function save() {
    saving = true;
    error = '';
    saved = false;
    const ok = await saveSiteNginx(site.domain, content);
    saving = false;
    if (ok) {
      saved = true;
      setTimeout(() => (saved = false), 2500);
    } else {
      error = m.sites_nginx_saveError();
    }
  }
</script>

<div class="p-3 sm:p-5 space-y-3 overflow-y-auto flex flex-col">
  <p class="text-[11px] text-gray-400 dark:text-gray-500 leading-relaxed">{m.sites_nginx_desc()}</p>

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

    {#if path}
      <p class="text-[10px] text-gray-400 dark:text-gray-600 font-mono break-all">{path}</p>
    {/if}

    <div class="flex items-center gap-3">
      <button
        onclick={save}
        disabled={saving}
        class="px-3 py-1.5 rounded-md text-xs font-semibold bg-sky-600 hover:bg-sky-500 text-white disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
      >
        {saving ? m.sites_nginx_saving() : m.sites_nginx_save()}
      </button>
      {#if saved}
        <span class="text-xs text-green-600 dark:text-green-400">{m.sites_nginx_saved()}</span>
      {/if}
      {#if error}
        <span class="text-xs text-red-500">{error}</span>
      {/if}
    </div>
  {/if}
</div>
