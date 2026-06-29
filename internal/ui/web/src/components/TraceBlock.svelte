<script lang="ts">
  import { openInEditor } from '$lib/editor';
  import type { QueryFrame } from '$lib/dumpsStream';
  import { m } from '../paraglide/messages.js';

  interface Props {
    src?: { file: string; line: number };
    trace?: QueryFrame[];
  }
  let { src, trace = [] }: Props = $props();
  let open = $state(false);

  // The most useful single frame: first application frame, then innermost, then src.
  const primary = $derived(
    trace.find((f) => !f.file.includes('/vendor/')) ??
      trace[0] ??
      (src?.file ? { func: '', file: src.file, line: src.line } : undefined)
  );
</script>

{#if primary}
  <div class="text-gray-700 dark:text-gray-200">
    {#if primary.func}<span class="font-semibold">{primary.func}</span> · {/if}
    <button
      type="button"
      class="font-mono text-lerd-red hover:underline break-all"
      onclick={() => openInEditor(primary.file, primary.line)}
      title={m.queries_openInEditor()}
    >{primary.file}:{primary.line}</button>
  </div>
{/if}
{#if trace.length > 1}
  <div>
    <button
      type="button"
      class="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 underline"
      onclick={() => (open = !open)}
    >{open ? m.queries_hideTrace() : m.queries_details()}</button>
    {#if open}
      <ol class="font-mono space-y-0.5 mt-1">
        {#each trace as frame}
          {@const app = !frame.file.includes('/vendor/')}
          <li class={app ? 'text-gray-700 dark:text-gray-200' : 'text-gray-400 dark:text-gray-500'}>
            <span class={app ? 'font-semibold' : ''}>{frame.func}</span> ·
            <button
              type="button"
              class="hover:underline break-all {app ? 'text-lerd-red' : ''}"
              onclick={() => openInEditor(frame.file, frame.line)}
              title={m.queries_openInEditor()}
            >{frame.file}:{frame.line}</button>
          </li>
        {/each}
      </ol>
    {/if}
  </div>
{/if}
