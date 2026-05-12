<script lang="ts">
  import { apiBase } from '$lib/api';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    domain: string;
    url: string;
    siteDomain?: string;
    branch?: string;
  }
  let { domain, url, siteDomain = '', branch = '' }: Props = $props();

  // QR lookup keys off the parent site's primary domain (the only one in the
  // registry). When this link is for a worktree, branch carries the
  // sanitized name so the backend can resolve the worktree port.
  const qrDomain = $derived(siteDomain || domain);
  const qrSrc = $derived(
    apiBase + '/api/lan-qr/' + qrDomain + (branch ? '?branch=' + encodeURIComponent(branch) : '')
  );

  let show = $state(false);
  let x = $state(0);
  let y = $state(0);

  function onEnter(e: Event) {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    x = r.left;
    y = r.bottom + 4;
    show = true;
  }
  function onLeave() {
    show = false;
  }
</script>

<div
  role="tooltip"
  onmouseenter={onEnter}
  onmouseleave={onLeave}
  onfocusin={onEnter}
  onfocusout={onLeave}
>
  <a href={url} target="_blank" rel="noopener" class="text-[10px] text-teal-600 dark:text-teal-400 font-mono hover:underline">{url}</a>
  {#if show}
    <div
      style="position:fixed; left:{x}px; top:{y}px; z-index:9999"
      class="p-1.5 bg-white dark:bg-lerd-card rounded-sm shadow-lg border border-gray-200 dark:border-lerd-border"
    >
      <img src={qrSrc} width="160" height="160" alt={m.lanShare_qrAlt()} />
    </div>
  {/if}
</div>
