<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import { type Site, installHorizonReloadWatcher } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  // Shown when the user enables Horizon auto-reload on a project without the
  // chokidar npm package (Vite 8 no longer ships it transitively). Offers a
  // one-click `npm install --save-dev chokidar`, then signals the parent to
  // enable reload once the watcher is present.
  interface Props {
    open: boolean;
    site: Site;
    onclose: () => void;
    oninstalled: () => void;
  }
  let { open, site, onclose, oninstalled }: Props = $props();

  let busy = $state(false);
  let error = $state('');

  $effect(() => {
    if (open) {
      busy = false;
      error = '';
    }
  });

  async function install() {
    if (busy) return;
    busy = true;
    error = '';
    const r = await installHorizonReloadWatcher(site);
    // If the user dismissed the modal (Escape/backdrop) while npm ran, don't
    // act on the result: chokidar still got installed, but we won't enable
    // reload behind their back.
    if (!open) return;
    busy = false;
    if (!r.ok) {
      error = r.error || m.horizonWatcher_installFailed();
      return;
    }
    oninstalled();
    onclose();
  }
</script>

<Modal {open} {onclose} title={m.horizonWatcher_title()} size="sm">
  <div class="px-5 py-4 space-y-3">
    <p class="text-sm text-gray-600 dark:text-gray-400">
      {m.horizonWatcher_body()}
    </p>
    <p class="text-[11px] text-gray-400 dark:text-gray-500">
      {m.horizonWatcher_command()}
    </p>
    {#if error}
      <pre class="text-[11px] text-red-600 dark:text-red-400 whitespace-pre-wrap break-words bg-red-50 dark:bg-red-900/15 rounded-sm px-2 py-1.5 max-h-32 overflow-y-auto">{error}</pre>
    {/if}
  </div>
  {#snippet footer()}
    <button
      type="button"
      onclick={onclose}
      disabled={busy}
      class="text-xs px-3 py-1.5 rounded-sm border border-gray-200 dark:border-lerd-border text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-white/5 transition-colors disabled:opacity-50"
    >{m.common_cancel()}</button>
    <button
      type="button"
      onclick={install}
      disabled={busy}
      class="inline-flex items-center gap-1.5 text-xs px-3 py-1.5 rounded-sm bg-emerald-600 hover:bg-emerald-500 text-white transition-colors disabled:opacity-60"
    >
      {#if busy}
        <svg class="w-3 h-3 animate-spin" fill="none" viewBox="0 0 24 24">
          <circle class="opacity-30" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
          <path class="opacity-90" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z" />
        </svg>
        {m.horizonWatcher_installing()}
      {:else}
        {m.horizonWatcher_installAction()}
      {/if}
    </button>
  {/snippet}
</Modal>
