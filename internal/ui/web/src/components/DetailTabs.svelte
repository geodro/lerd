<script lang="ts" module>
  export interface TabItem<T extends string = string> {
    id: T;
    label: string;
    hidden?: boolean;
  }
</script>

<script lang="ts" generics="T extends string">
  interface Props {
    tabs: TabItem<T>[];
    active: T;
    onchange: (id: T) => void;
  }
  let { tabs, active, onchange }: Props = $props();

  // A lone tab can't be switched to anything, so the bar is just noise. Hide it
  // (and the empty 0-tab case) and let the content fill the space instead.
  const visible = $derived(tabs.filter((t) => !t.hidden));
</script>

{#if visible.length > 1}
  <div class="flex items-end gap-4 border-b border-gray-100 dark:border-lerd-border pt-3 px-3 shrink-0">
    {#each visible as t (t.id)}
      <button
        onclick={() => onchange(t.id)}
        class="pb-1 text-xs font-medium transition-colors border-b-2 {active === t.id
          ? 'border-lerd-red text-lerd-red'
          : 'border-transparent text-gray-500 hover:text-gray-700 dark:hover:text-gray-300'}"
      >{t.label}</button>
    {/each}
  </div>
{/if}
