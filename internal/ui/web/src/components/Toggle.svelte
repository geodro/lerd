<script lang="ts" module>
  export type ToggleTone = 'accent' | 'amber' | 'teal' | 'red' | 'violet' | 'emerald' | 'sky' | 'indigo';
</script>

<script lang="ts">
  interface Props {
    on: boolean;
    tone?: ToggleTone;
    failing?: boolean;
    loading?: boolean;
    disabled?: boolean;
    title?: string;
    onclick?: (e: MouseEvent) => void;
  }

  let {
    on,
    tone = 'accent',
    failing = false,
    loading = false,
    disabled = false,
    title,
    onclick
  }: Props = $props();

  const onClass: Record<ToggleTone, string> = {
    accent: 'bg-lerd-red',
    amber: 'bg-amber-500',
    teal: 'bg-teal-500',
    red: 'bg-red-500',
    violet: 'bg-violet-500',
    emerald: 'bg-emerald-500',
    sky: 'bg-sky-500',
    indigo: 'bg-indigo-500'
  };

  const bgClass = $derived(
    failing ? 'bg-red-500 animate-pulse' : on ? onClass[tone] : 'bg-gray-300 dark:bg-lerd-muted'
  );
</script>

<button
  {title}
  {onclick}
  disabled={disabled || loading}
  class="relative inline-flex h-4 w-7 shrink-0 items-center rounded-full transition-colors duration-200 focus:outline-none disabled:opacity-50 {bgClass}"
>
  <span
    class="inline-block h-3 w-3 rounded-full bg-white shadow transition-transform duration-200 {on || failing
      ? 'translate-x-3.5'
      : 'translate-x-0.5'}"
  ></span>
  {#if loading}
    <svg class="animate-spin absolute right-[-14px] w-2.5 h-2.5 text-gray-400" fill="none" viewBox="0 0 24 24">
      <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"/>
      <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"/>
    </svg>
  {/if}
</button>
