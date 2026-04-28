<script lang="ts">
  import { apiBase } from '$lib/api';

  interface Props {
    domain: string;
    url: string;
  }
  let { domain, url }: Props = $props();

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
      class="p-1.5 bg-white dark:bg-lerd-card rounded shadow-lg border border-gray-200 dark:border-lerd-border"
    >
      <img src={apiBase + '/api/lan-qr/' + domain} width="160" height="160" alt="QR code" />
    </div>
  {/if}
</div>
