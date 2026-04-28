<script lang="ts">
  import type { Site } from '$stores/sites';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  let show = $state(false);

  const extras = $derived((site.domains || []).slice(1));
  const conflicts = $derived(site.conflicting_domains || []);
  const count = $derived(extras.length + conflicts.length);
  const hasConflicts = $derived(conflicts.length > 0);

  function scheme(): string {
    return site.tls ? 'https://' : 'http://';
  }
</script>

{#if count > 0}
  <span
    class="relative cursor-default"
    role="tooltip"
    onmouseenter={() => (show = true)}
    onmouseleave={() => (show = false)}
    onfocus={() => (show = true)}
    onblur={() => (show = false)}
  >
    <span
      class="inline-flex items-center gap-1 text-xs {hasConflicts
        ? 'text-amber-600 dark:text-amber-400'
        : 'text-gray-400 dark:text-gray-500'}"
    >
      {#if hasConflicts}
        <svg class="w-3 h-3 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01M4.93 19h14.14a2 2 0 001.74-3l-7.07-12a2 2 0 00-3.48 0L3.19 16a2 2 0 001.74 3z"/>
        </svg>
      {/if}
      +{count} more
    </span>
    {#if show}
      <div class="absolute left-0 top-full z-50 pt-1">
        <div class="bg-gray-900 dark:bg-gray-800 text-white text-xs rounded-lg px-3 py-2 shadow-lg whitespace-nowrap">
        {#each extras as d (d)}
          <a
            href={scheme() + d}
            target="_blank"
            rel="noopener"
            class="flex items-center gap-1.5 py-0.5 font-mono text-white hover:text-lerd-red transition-colors cursor-pointer"
          >
            <span>{d}</span>
            <svg class="w-3 h-3 shrink-0 opacity-60" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14"/>
            </svg>
          </a>
        {/each}
        {#each conflicts as c (c.domain)}
          <div class="flex items-center gap-1.5 py-0.5 font-mono">
            <svg class="w-3 h-3 shrink-0 text-amber-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01M4.93 19h14.14a2 2 0 001.74-3l-7.07-12a2 2 0 00-3.48 0L3.19 16a2 2 0 001.74 3z"/>
            </svg>
            <span class="line-through text-gray-400">{c.domain}</span>
            {#if c.owned_by}
              <span class="text-amber-300">→ {c.owned_by}</span>
            {/if}
          </div>
        {/each}
        </div>
      </div>
    {/if}
  </span>
{/if}
