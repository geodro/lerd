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
</script>

<div class="flex gap-0 border-b border-gray-100 dark:border-lerd-border pt-3 px-3 sm:px-5 shrink-0">
  {#each tabs as t (t.id)}
    {#if !t.hidden}
      <button
        onclick={() => onchange(t.id)}
        class="pb-2 mr-5 text-xs font-medium transition-colors border-b-2 {active === t.id
          ? 'border-lerd-red text-lerd-red'
          : 'border-transparent text-gray-500 hover:text-gray-700 dark:hover:text-gray-300'}"
      >{t.label}</button>
    {/if}
  {/each}
</div>
