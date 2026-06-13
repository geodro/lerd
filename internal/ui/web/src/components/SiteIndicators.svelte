<script lang="ts">
  import StatusDot from '$components/StatusDot.svelte';
  import { runningWorkerColors, idleWorkerColors, siteWorkerFailing, siteHasWorkers, type Site } from '$stores/sites';
  import { m } from '../paraglide/messages.js';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  const dots = $derived(runningWorkerColors(site));
  // While asleep, keep showing the suspended workers' dots (dimmed) so the site
  // doesn't look like it lost its workers.
  const idleDots = $derived(idleWorkerColors(site));
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
{#if site.idle && siteHasWorkers(site)}
  <!-- Asleep: keep the worker dots (dimmed) and float a moon above them. A site
       with no workers never sleeps, so it gets no moon at all. -->
  {#if idleDots.length > 0}
    <span class="relative inline-flex items-center shrink-0" title={m.sites_idleHint()}>
      <span class="absolute -top-2.5 left-1/2 -translate-x-1/2 text-sky-500 dark:text-sky-400">
        <svg class="w-3.5 h-3.5 drop-shadow-[0_1px_2px_rgba(0,0,0,0.45)]" viewBox="0 0 24 24" fill="currentColor">
          <path d="M21.752 15.002A9.72 9.72 0 0 1 18 15.75c-5.385 0-9.75-4.365-9.75-9.75 0-1.33.266-2.597.748-3.752A9.753 9.753 0 0 0 3 11.25C3 16.635 7.365 21 12.75 21a9.753 9.753 0 0 0 9.002-5.998z" />
        </svg>
      </span>
      <span class="inline-flex items-center gap-1 opacity-50">
        {#each idleDots as c, i (i + ':' + c)}
          <StatusDot color={c} size="xs" />
        {/each}
      </span>
    </span>
  {:else}
    <span class="inline-flex shrink-0 text-sky-500 dark:text-sky-400" title={m.sites_idleHint()}>
      <svg class="w-4 h-4" viewBox="0 0 24 24" fill="currentColor">
        <path d="M21.752 15.002A9.72 9.72 0 0 1 18 15.75c-5.385 0-9.75-4.365-9.75-9.75 0-1.33.266-2.597.748-3.752A9.753 9.753 0 0 0 3 11.25C3 16.635 7.365 21 12.75 21a9.753 9.753 0 0 0 9.002-5.998z" />
      </svg>
    </span>
  {/if}
{:else}
  {#each dots as c, i (i + ':' + c)}
    <StatusDot color={c} size="xs" />
  {/each}
{/if}
