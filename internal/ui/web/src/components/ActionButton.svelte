<script lang="ts">
  import type { Snippet } from 'svelte';

  type Tone = 'default' | 'accent' | 'success' | 'danger';

  interface Props {
    title: string;
    tone?: Tone;
    disabled?: boolean;
    loading?: boolean;
    onclick?: (e: MouseEvent) => void;
    children: Snippet;
  }

  let {
    title,
    tone = 'default',
    disabled = false,
    loading = false,
    onclick,
    children
  }: Props = $props();

  const toneClass: Record<Tone, string> = {
    default:
      'text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 hover:bg-gray-100 dark:hover:bg-white/5',
    accent:
      'text-gray-400 hover:text-lerd-red hover:bg-gray-100 dark:hover:bg-white/5',
    success:
      'text-emerald-600 hover:bg-emerald-50 dark:hover:bg-emerald-900/20',
    danger: 'text-red-500 hover:bg-red-50 dark:hover:bg-red-900/20'
  };
</script>

<button
  {title}
  {onclick}
  {disabled}
  class="w-6 h-6 flex items-center justify-center rounded transition-colors disabled:opacity-40 {toneClass[tone]}"
>
  {#if loading}
    <svg class="w-3.5 h-3.5 animate-spin" fill="none" viewBox="0 0 24 24">
      <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
      <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z" />
    </svg>
  {:else}
    {@render children()}
  {/if}
</button>
