<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    title?: string;
    active?: boolean;
    onclick?: (e: MouseEvent) => void;
    size?: 'sm' | 'md' | 'lg';
    children: Snippet;
  }

  let { title, active = false, onclick, size = 'md', children }: Props = $props();

  const sizeMap: Record<NonNullable<Props['size']>, string> = {
    sm: 'w-8 h-8',
    md: 'w-10 h-10',
    lg: 'w-12 h-12'
  };

  const sizeClass = $derived(sizeMap[size]);
</script>

<button
  {title}
  {onclick}
  class="{sizeClass} rounded-xl flex items-center justify-center transition-colors {active
    ? 'bg-lerd-red/10 text-lerd-red'
    : 'text-gray-400 dark:text-gray-500 hover:bg-gray-100 dark:hover:bg-white/5 hover:text-gray-700 dark:hover:text-gray-300'}"
>
  {@render children()}
</button>
