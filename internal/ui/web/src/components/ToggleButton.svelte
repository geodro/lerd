<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    label: string;
    on: boolean;
    failing?: boolean;
    loading?: boolean;
    disabled?: boolean;
    title?: string;
    // Rounding/border utility classes for the outer button. Defaults to a
    // standalone pill; pass e.g. 'rounded-l-md border-r-0' to join it into a
    // segmented control.
    rounding?: string;
    onclick?: (e: MouseEvent) => void;
    trailing?: Snippet;
  }
  let {
    label,
    on,
    failing = false,
    loading = false,
    disabled = false,
    title = '',
    rounding = 'rounded-md',
    onclick,
    trailing
  }: Props = $props();

  const state = $derived(
    loading ? 'loading' : failing ? 'failing' : on ? 'on' : 'off'
  );

  const dotClass = $derived(
    state === 'on'
      ? 'bg-emerald-500'
      : state === 'failing'
        ? 'bg-red-500'
        : state === 'loading'
          ? 'bg-amber-400'
          : 'border border-gray-300 dark:border-gray-600 bg-transparent'
  );

  const tintClass = $derived(
    state === 'on'
      ? 'bg-emerald-50/60 dark:bg-emerald-900/15 hover:bg-emerald-50 dark:hover:bg-emerald-900/25'
      : state === 'failing'
        ? 'bg-red-50/60 dark:bg-red-900/15 hover:bg-red-50 dark:hover:bg-red-900/25'
        : 'bg-white dark:bg-lerd-card hover:bg-gray-50 dark:hover:bg-white/5'
  );
</script>

<button
  type="button"
  {disabled}
  {title}
  {onclick}
  class="inline-flex items-center gap-1.5 h-7 px-2.5 {rounding} border border-gray-200 dark:border-lerd-border transition-colors text-xs font-medium text-gray-700 dark:text-gray-200 disabled:opacity-50 disabled:cursor-not-allowed {tintClass}"
>
  {#if state === 'loading'}
    <svg class="w-2.5 h-2.5 animate-spin text-amber-500" fill="none" viewBox="0 0 24 24">
      <circle class="opacity-30" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
      <path class="opacity-90" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z" />
    </svg>
  {:else}
    <span class="shrink-0 w-2 h-2 rounded-full {dotClass}"></span>
  {/if}
  <span>{label}</span>
  {#if trailing}{@render trailing()}{/if}
</button>
