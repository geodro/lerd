<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import SiteNginxTab from '$tabs/sites/SiteNginxTab.svelte';
  import PhpIniTab from '$tabs/system/PhpIniTab.svelte';
  import { type Site } from '$stores/sites';
  import { modal } from '$stores/modals';
  import { m } from '../paraglide/messages.js';

  interface Props {
    site: Site;
    /** Domain to edit — the active worktree's domain, or the site's primary. */
    domain: string;
    open: boolean;
    onclose: () => void;
  }
  let { site, domain, open, onclose }: Props = $props();

  // FrankenPHP sites run their own container instead of the shared FPM pool, so
  // their php.ini isn't reachable from the System → PHP per-version editor the
  // way FPM users edit it. Surface that same per-version editor here as a second
  // tab; the FrankenPHP container mounts the shared 98-lerd-user.ini, so edits
  // apply to it after the save restarts the container.
  const showPhpIni = $derived(site.runtime === 'frankenphp' && Boolean(site.php_version));
  let tab = $state<'nginx' | 'phpini'>('nginx');

  // Save/Restore/Reset open their own confirm modal on top of this one via
  // the shared modal store. While one is up it owns Escape and the backdrop,
  // so don't let the editor close out from under it and discard the buffer.
  function handleClose() {
    if ($modal.kind) return;
    onclose();
  }

  const tabBtn = (id: 'nginx' | 'phpini') =>
    'px-3 py-1.5 text-xs font-medium border-b-2 transition-colors ' +
    (tab === id
      ? 'border-lerd-red text-lerd-red'
      : 'border-transparent text-gray-500 hover:text-gray-700 dark:hover:text-gray-300');
</script>

<Modal {open} title={m.sites_nginx_modalTitle({ domain })} onclose={handleClose} size="xl">
  <div class="h-[70vh] flex flex-col overflow-hidden rounded-b-xl">
    {#if showPhpIni}
      <div class="flex gap-1 px-3 pt-1 border-b border-gray-200 dark:border-gray-700 shrink-0">
        <button class={tabBtn('nginx')} onclick={() => (tab = 'nginx')}>Nginx</button>
        <button class={tabBtn('phpini')} onclick={() => (tab = 'phpini')}>php.ini</button>
      </div>
    {/if}
    <div class="flex-1 min-h-0 overflow-hidden">
      {#if showPhpIni && tab === 'phpini'}
        <!-- Per-site scope: a FrankenPHP site has its own php.ini, edited via the
             same per-version editor keyed by a "site:<name>" scope token. -->
        <PhpIniTab version={`site:${site.name}`} />
      {:else}
        {#key domain}
          <SiteNginxTab {site} {domain} onSaved={onclose} />
        {/key}
      {/if}
    </div>
  </div>
</Modal>
