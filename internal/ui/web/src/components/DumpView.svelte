<script lang="ts" module>
  import type { DumpNode } from '$lib/dump-parser';
  export type { DumpNode };
</script>

<script lang="ts">
  import { untrack } from 'svelte';
  import DumpView from './DumpView.svelte';

  interface Props {
    node: DumpNode;
    depth?: number;
    initiallyOpen?: boolean;
  }
  let { node, depth = 0, initiallyOpen = true }: Props = $props();

  // Auto-collapse anything past the second level so deep trees don't blow
  // up on first render. One-shot initialization, no re-tracking on prop
  // changes (the user's open/closed state stays sticky).
  let open = $state(untrack(() => initiallyOpen && depth < 2));

  function visBadge(v: string) {
    if (v === 'public') return '+';
    if (v === 'protected') return '#';
    if (v === 'private') return '-';
    if (v === 'readonly') return '~';
    return '';
  }
  function visColor(v: string) {
    if (v === 'public') return 'text-emerald-600 dark:text-emerald-400';
    if (v === 'protected') return 'text-sky-600 dark:text-sky-400';
    if (v === 'private') return 'text-rose-600 dark:text-rose-400';
    if (v === 'readonly') return 'text-amber-600 dark:text-amber-400';
    return 'text-gray-400';
  }
  function scalarColor(t: string) {
    switch (t) {
      case 'string': return 'text-emerald-700 dark:text-emerald-300';
      case 'number': return 'text-violet-700 dark:text-violet-300';
      case 'bool':   return 'text-amber-700 dark:text-amber-300';
      case 'null':   return 'text-gray-400 italic';
      default:       return 'text-gray-700 dark:text-gray-200';
    }
  }
</script>

{#if node.kind === 'scalar'}
  <span class={scalarColor(node.type)}>{node.value}</span>
{:else if node.kind === 'array'}
  {#if node.count === 0}
    <span class="text-gray-400">[]</span>
  {:else}
    <button
      type="button"
      class="inline-flex items-center gap-1 text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200"
      onclick={() => (open = !open)}
    >
      <span class="text-[10px] w-3 inline-block">{open ? '▾' : '▸'}</span>
      <span class="text-violet-600 dark:text-violet-400">array:{node.count}</span>
      <span>{open ? '[' : '[…]'}</span>
    </button>
    {#if open}
      <div class="ml-4 border-l border-gray-200 dark:border-lerd-border pl-3">
        {#each node.items as item, i (i)}
          <div class="flex flex-wrap items-start gap-x-2">
            <span class="text-gray-400 select-none">{item.key}</span>
            <span class="text-gray-400">⇒</span>
            <DumpView node={item.value} depth={depth + 1} {initiallyOpen} />
          </div>
        {/each}
      </div>
      <span class="text-gray-500">]</span>
    {/if}
  {/if}
{:else}
  {@const isEmpty = node.props.length === 0}
  {#if node.ref || isEmpty}
    <span class="text-sky-700 dark:text-sky-300">{node.class}</span><!--
    -->{#if node.id !== undefined}<span class="text-gray-400"> &#123;#{node.id}{node.ref && !isEmpty ? ' …' : ''}&#125;</span>{/if}
  {:else}
    <button
      type="button"
      class="inline-flex items-center gap-1 hover:opacity-80"
      onclick={() => (open = !open)}
    >
      <span class="text-[10px] w-3 inline-block text-gray-500">{open ? '▾' : '▸'}</span>
      <span class="text-sky-700 dark:text-sky-300">{node.class}</span>
      {#if node.id !== undefined}<span class="text-gray-400">&#123;#{node.id}{open ? '' : ' …'}&#125;</span>{/if}
    </button>
    {#if open}
      <div class="ml-4 border-l border-gray-200 dark:border-lerd-border pl-3">
        {#each node.props as prop, i (i)}
          <div class="flex flex-wrap items-start gap-x-2">
            <span class="select-none {visColor(prop.visibility)}" title={prop.visibility}>{visBadge(prop.visibility)}</span>
            <span class="text-gray-700 dark:text-gray-300">{prop.name}:</span>
            <DumpView node={prop.value} depth={depth + 1} {initiallyOpen} />
          </div>
        {/each}
      </div>
    {/if}
  {/if}
{/if}
