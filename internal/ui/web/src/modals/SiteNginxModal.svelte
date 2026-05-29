<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import SiteNginxTab from '$tabs/sites/SiteNginxTab.svelte';
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

  // Save/Restore/Reset open their own confirm modal on top of this one via
  // the shared modal store. While one is up it owns Escape and the backdrop,
  // so don't let the editor close out from under it and discard the buffer.
  function handleClose() {
    if ($modal.kind) return;
    onclose();
  }
</script>

<Modal {open} title={m.sites_nginx_modalTitle({ domain })} onclose={handleClose} size="xl">
  <div class="h-[70vh] flex flex-col overflow-hidden rounded-b-xl">
    {#key domain}
      <SiteNginxTab {site} {domain} onSaved={onclose} />
    {/key}
  </div>
</Modal>
