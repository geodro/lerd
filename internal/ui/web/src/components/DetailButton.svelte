<script lang="ts" module>
  export type DetailButtonTone = 'primary' | 'secondary' | 'success' | 'danger' | 'warn' | 'info';
</script>

<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    tone?: DetailButtonTone;
    disabled?: boolean;
    loading?: boolean;
    title?: string;
    onclick?: (e: MouseEvent) => void;
    href?: string;
    target?: string;
    icon?: Snippet;
    children?: Snippet;
  }

  let {
    tone = 'secondary',
    disabled = false,
    loading = false,
    title,
    onclick,
    href,
    target,
    icon,
    children
  }: Props = $props();

  const toneClass: Record<DetailButtonTone, string> = {
    primary: 'bg-lerd-red hover:bg-lerd-redhov text-white',
    secondary:
      'bg-gray-100 dark:bg-white/5 hover:bg-gray-200 dark:hover:bg-white/10 text-gray-700 dark:text-gray-300 border border-gray-200 dark:border-lerd-border',
    success: 'bg-emerald-600 hover:bg-emerald-700 text-white',
    danger:
      'bg-gray-100 dark:bg-white/5 hover:bg-red-50 dark:hover:bg-red-500/10 hover:text-red-600 dark:hover:text-red-400 hover:border-red-200 dark:hover:border-red-500/30 text-gray-600 dark:text-gray-400 border border-gray-200 dark:border-lerd-border',
    warn:
      'bg-amber-50 dark:bg-amber-500/10 border border-amber-300 dark:border-amber-500/40 text-amber-600 dark:text-amber-400 hover:bg-amber-100 dark:hover:bg-amber-500/20',
    info:
      'bg-sky-50 dark:bg-sky-500/10 hover:bg-sky-100 dark:hover:bg-sky-500/20 text-sky-700 dark:text-sky-400 border border-sky-200 dark:border-sky-500/30'
  };

  const cls = $derived(
    'inline-flex items-center gap-1.5 text-xs font-medium rounded-lg px-3 py-1.5 transition-colors disabled:opacity-50 ' +
      toneClass[tone]
  );
</script>

{#snippet inner()}
  {#if loading}
    <svg class="w-3 h-3 animate-spin" fill="none" viewBox="0 0 24 24">
      <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"/>
      <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"/>
    </svg>
  {:else}
    {#if icon}{@render icon()}{/if}
    {#if children}{@render children()}{/if}
  {/if}
{/snippet}

{#if href}
  <a {href} {target} {title} class={cls}>{@render inner()}</a>
{:else}
  <button {title} {onclick} {disabled} class={cls}>{@render inner()}</button>
{/if}
