<script lang="ts">
  import StatusDot from '$components/StatusDot.svelte';
  import { runningWorkerColors, siteWorkerFailing, type Site } from '$stores/sites';
  import { m } from '../paraglide/messages.js';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  const dots = $derived(runningWorkerColors(site));
</script>

{#if site.worktrees && site.worktrees.length > 0}
  <span title={m.sites_gitWorktrees()} class="inline-flex shrink-0">
    <svg class="w-3 h-3 text-violet-400" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">
      <path d="M6 3v12M15 6a3 3 0 1 0 6 0a3 3 0 1 0-6 0M3 18a3 3 0 1 0 6 0a3 3 0 1 0-6 0M18 9a9 9 0 0 1-9 9"/>
    </svg>
  </span>
{/if}
{#if siteWorkerFailing(site)}
  <span title={m.sites_workerFailing()} class="shrink-0"><StatusDot color="red" size="xs" pulse /></span>
{/if}
{#each dots as c, i (i + ':' + c)}
  <StatusDot color={c} size="xs" />
{/each}
