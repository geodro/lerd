<script lang="ts" module>
  export type CardTone = 'default' | 'critical' | 'warn';
</script>

<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    title: string;
    tone?: CardTone;
    badge?: Snippet;
    footer?: Snippet;
    children: Snippet;
  }
  let { title, tone = 'default', badge, footer, children }: Props = $props();

  const accent: Record<CardTone, string> = {
    default: '',
    critical: 'border-l-4 border-l-red-500',
    warn: 'border-l-4 border-l-yellow-500'
  };
</script>

<div class="flex flex-col max-h-[340px] bg-white dark:bg-lerd-card border border-gray-100 dark:border-lerd-border rounded-xl overflow-hidden {accent[tone]}">
  <div class="shrink-0 flex items-center justify-between gap-3 px-3 py-2.5 border-b border-gray-100 dark:border-lerd-border">
    <span class="text-sm font-semibold text-gray-700 dark:text-gray-200">{title}</span>
    {#if badge}{@render badge()}{/if}
  </div>
  <div class="flex-1 min-h-0 overflow-y-auto px-3 py-3 space-y-2.5">
    {@render children()}
  </div>
  {#if footer}
    <div class="shrink-0 px-3 py-2.5 border-t border-gray-100 dark:border-lerd-border">
      {@render footer()}
    </div>
  {/if}
</div>
