<script lang="ts" module>
  export type BadgeTone =
    | 'running'
    | 'stopped'
    | 'paused'
    | 'framework'
    | 'frankenphp'
    | 'xdebug-on'
    | 'xdebug-off'
    | 'neutral'
    | 'branch';
</script>

<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    tone: BadgeTone;
    dot?: boolean;
    title?: string;
    onclick?: (e: MouseEvent) => void;
    children: Snippet;
  }
  let { tone, dot = false, title, onclick, children }: Props = $props();

  const toneClass: Record<BadgeTone, string> = {
    running: 'text-emerald-600 dark:text-emerald-500 bg-emerald-50 dark:bg-emerald-900/20',
    stopped: 'text-red-500 bg-red-50 dark:bg-red-900/20',
    paused: 'text-amber-600 dark:text-amber-400 bg-amber-50 dark:bg-amber-900/20',
    framework: 'text-lerd-red bg-red-50 dark:bg-red-900/20',
    frankenphp:
      'text-orange-700 dark:text-orange-300 bg-orange-50 dark:bg-orange-500/10 border border-orange-200 dark:border-orange-500/30',
    'xdebug-on':
      'text-purple-700 dark:text-purple-300 bg-purple-50 dark:bg-purple-900/20 border border-purple-200 dark:border-purple-500/40 hover:bg-purple-100 dark:hover:bg-purple-900/40',
    'xdebug-off':
      'text-gray-500 dark:text-gray-400 bg-gray-50 dark:bg-white/5 border border-gray-200 dark:border-lerd-border hover:bg-gray-100 dark:hover:bg-white/10',
    neutral:
      'text-gray-600 dark:text-gray-400 bg-gray-100 dark:bg-white/5 border border-gray-200 dark:border-lerd-border',
    branch: 'text-violet-500 dark:text-violet-400'
  };

  const dotColor: Record<BadgeTone, string> = {
    running: 'bg-emerald-500',
    stopped: 'bg-red-500',
    paused: 'bg-amber-500',
    framework: 'bg-lerd-red',
    frankenphp: 'bg-orange-500',
    'xdebug-on': 'bg-purple-500',
    'xdebug-off': 'bg-gray-400 dark:bg-gray-600',
    neutral: 'bg-gray-400',
    branch: 'bg-violet-500'
  };

  const base = 'inline-flex items-center gap-1 text-xs font-medium px-2 py-0.5 rounded-full transition-colors';
</script>

{#if onclick}
  <button {onclick} {title} class="{base} {toneClass[tone]}">
    {#if dot}<span class="w-1.5 h-1.5 rounded-full {dotColor[tone]}"></span>{/if}
    {@render children()}
  </button>
{:else}
  <span {title} class="{base} {toneClass[tone]}">
    {#if dot}<span class="w-1.5 h-1.5 rounded-full {dotColor[tone]}"></span>{/if}
    {@render children()}
  </span>
{/if}
