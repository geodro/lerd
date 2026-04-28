<script lang="ts">
  import { onMount, onDestroy, type Snippet } from 'svelte';
  import Icon from './Icon.svelte';

  interface Props {
    open: boolean;
    title: string;
    onclose: () => void;
    size?: 'sm' | 'md' | 'lg';
    children: Snippet;
    footer?: Snippet;
  }
  let { open, title, onclose, size = 'md', children, footer }: Props = $props();

  const widthClass = $derived(
    size === 'sm' ? 'max-w-sm' : size === 'lg' ? 'max-w-2xl' : 'max-w-lg'
  );

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && open) onclose();
  }

  onMount(() => window.addEventListener('keydown', onKey));
  onDestroy(() => window.removeEventListener('keydown', onKey));
</script>

{#if open}
  <div class="fixed inset-0 z-50 flex items-center justify-center">
    <button
      class="absolute inset-0 bg-black/50"
      aria-label="Close"
      onclick={onclose}
    ></button>
    <div
      class="relative bg-white dark:bg-lerd-card border border-gray-200 dark:border-lerd-border rounded-xl shadow-2xl w-full {widthClass} mx-4"
    >
      <div class="flex items-center justify-between px-5 py-4 border-b border-gray-100 dark:border-lerd-border">
        <h3 class="font-semibold text-gray-900 dark:text-white">{title}</h3>
        <button
          onclick={onclose}
          class="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 transition-colors"
          title="Close"
        >
          <Icon name="close" class="w-5 h-5" />
        </button>
      </div>
      {@render children()}
      {#if footer}
        <div class="px-5 py-3 border-t border-gray-100 dark:border-lerd-border flex items-center justify-end gap-2">
          {@render footer()}
        </div>
      {/if}
    </div>
  </div>
{/if}
