<script lang="ts" module>
  import type { Snippet } from 'svelte';
  import type { DetailButtonTone } from './DetailButton.svelte';

  export interface ButtonMenuAction {
    id: string;
    label: string;
    description?: string;
    icon?: Snippet;
    tone?: DetailButtonTone;
    disabled?: boolean;
    href?: string;
    target?: string;
    title?: string;
    onclick?: () => void;
  }

  export const buttonMenuToneClass: Record<DetailButtonTone, string> = {
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
</script>

<script lang="ts">
  import { m } from '../paraglide/messages.js';

  interface Props {
    actions: ButtonMenuAction[];
    busy?: boolean;
    menuLabel?: string;
  }
  let { actions, busy = false, menuLabel }: Props = $props();

  const primary = $derived(actions[0]);
  const rest = $derived(actions.slice(1));

  let open = $state(false);
  let rootEl: HTMLDivElement | undefined = $state();

  function close() { open = false; }
  function toggle() { open = !open; }

  function onDocClick(e: MouseEvent) {
    if (!rootEl) return;
    if (!rootEl.contains(e.target as Node)) close();
  }
  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape') close();
  }

  $effect(() => {
    if (!open) return;
    document.addEventListener('mousedown', onDocClick);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDocClick);
      document.removeEventListener('keydown', onKey);
    };
  });

  function runItem(a: ButtonMenuAction) {
    close();
    if (a.disabled || busy) return;
    if (a.href) {
      window.open(a.href, a.target ?? '_blank', 'noopener,noreferrer');
      return;
    }
    a.onclick?.();
  }

  const baseBtn =
    'inline-flex items-center gap-1.5 text-xs font-medium px-3 py-1.5 transition-colors disabled:opacity-50';
</script>

{#snippet spinner()}
  <svg class="w-3 h-3 animate-spin" fill="none" viewBox="0 0 24 24">
    <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"/>
    <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"/>
  </svg>
{/snippet}

{#if actions.length === 0}
  {''}
{:else if actions.length === 1}
  {@const only = actions[0]}
  {@const tone = only.tone ?? 'secondary'}
  {#if only.href}
    <a
      class="{baseBtn} rounded-lg {buttonMenuToneClass[tone]}"
      href={only.href}
      target={only.target}
      title={only.title}
    >
      {#if only.icon}{@render only.icon()}{/if}
      {only.label}
    </a>
  {:else}
    <button
      type="button"
      class="{baseBtn} rounded-lg {buttonMenuToneClass[tone]}"
      onclick={only.onclick}
      disabled={only.disabled || busy}
      title={only.title}
    >
      {#if busy}
        {@render spinner()}
      {:else}
        {#if only.icon}{@render only.icon()}{/if}
        {only.label}
      {/if}
    </button>
  {/if}
{:else}
  {@const tone = primary.tone ?? 'secondary'}
  <div bind:this={rootEl} class="relative inline-flex">
    {#if primary.href}
      <a
        class="{baseBtn} rounded-l-lg {buttonMenuToneClass[tone]}"
        href={primary.href}
        target={primary.target}
        title={primary.title}
      >
        {#if primary.icon}{@render primary.icon()}{/if}
        {primary.label}
      </a>
    {:else}
      <button
        type="button"
        class="{baseBtn} rounded-l-lg {buttonMenuToneClass[tone]}"
        onclick={primary.onclick}
        disabled={primary.disabled || busy}
        title={primary.title}
      >
        {#if busy}
          {@render spinner()}
        {:else}
          {#if primary.icon}{@render primary.icon()}{/if}
          {primary.label}
        {/if}
      </button>
    {/if}
    <button
      type="button"
      onclick={toggle}
      class="{baseBtn} rounded-r-lg border-l border-black/10 dark:border-white/10 px-1.5 {buttonMenuToneClass[tone]}"
      aria-haspopup="menu"
      aria-expanded={open}
      aria-label={menuLabel ?? m.common_moreActions()}
      title={menuLabel ?? m.common_moreActions()}
      data-testid="button-menu-toggle"
    >
      <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
        <polyline points="6 9 12 15 18 9"/>
      </svg>
    </button>

    {#if open}
      <div
        role="menu"
        data-testid="button-menu-list"
        class="absolute right-0 top-full mt-2 z-50 min-w-64 max-w-xs rounded-xl bg-white dark:bg-lerd-card border border-gray-200 dark:border-lerd-border shadow-xl py-1 overflow-hidden"
      >
        {#each rest as a (a.id)}
          <button
            type="button"
            role="menuitem"
            class="w-full text-left px-4 py-2.5 hover:bg-gray-50 dark:hover:bg-white/5 disabled:opacity-50 disabled:hover:bg-transparent flex items-start gap-2"
            onclick={() => runItem(a)}
            disabled={a.disabled || busy}
            title={a.title}
          >
            <div class="flex-1 min-w-0">
              <div class="flex items-center gap-2 text-sm font-medium text-gray-900 dark:text-white">
                {#if a.icon}<span class="shrink-0">{@render a.icon()}</span>{/if}
                <span class="truncate">{a.label}</span>
              </div>
              {#if a.description}
                <div class="text-xs text-gray-500 dark:text-gray-400 mt-0.5">{a.description}</div>
              {/if}
            </div>
          </button>
        {/each}
      </div>
    {/if}
  </div>
{/if}
