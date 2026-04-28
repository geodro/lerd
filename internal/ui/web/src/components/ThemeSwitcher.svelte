<script lang="ts">
  import { theme, type Theme } from '$stores/theme';
  import { m } from '../paraglide/messages.js';

  interface Props {
    size?: 'sm' | 'md';
  }
  let { size = 'sm' }: Props = $props();

  const modes: Theme[] = ['light', 'auto', 'dark'];

  const labels = $derived<Record<Theme, string>>({
    light: m.theme_light(),
    auto: m.theme_auto(),
    dark: m.theme_dark()
  });

  const containerClass = $derived(
    size === 'sm'
      ? 'flex flex-col rounded-md border border-gray-200 dark:border-lerd-border overflow-hidden'
      : 'flex flex-row rounded-md border border-gray-200 dark:border-lerd-border overflow-hidden text-xs'
  );

  const cellClass = $derived(
    size === 'sm'
      ? 'px-2 py-1.5 text-[10px] font-medium transition-colors'
      : 'px-2 py-1 capitalize transition-colors'
  );
</script>

<div class={containerClass}>
  {#each modes as mode (mode)}
    <button
      title={labels[mode]}
      class="{cellClass} {$theme === mode
        ? 'bg-gray-200 dark:bg-white/10 text-gray-900 dark:text-white'
        : 'text-gray-400 dark:text-gray-500 hover:bg-gray-100 dark:hover:bg-white/5'}"
      onclick={() => theme.set(mode)}
    >
      {mode[0].toUpperCase()}
    </button>
  {/each}
</div>
