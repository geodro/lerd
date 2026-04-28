<script lang="ts">
  interface Props {
    vars: Record<string, string>;
    label?: string;
  }
  let { vars, label = '.env' }: Props = $props();

  const text = $derived(
    Object.keys(vars)
      .sort()
      .map((k) => `${k}=${vars[k]}`)
      .join('\n')
  );

  let copied = $state(false);
  let resetTimer: ReturnType<typeof setTimeout> | null = null;

  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
      copied = true;
      if (resetTimer) clearTimeout(resetTimer);
      resetTimer = setTimeout(() => (copied = false), 1500);
    } catch {
      /* no-op */
    }
  }
</script>

<div class="rounded-lg border border-gray-200 dark:border-lerd-border overflow-hidden">
  <div
    class="flex items-center justify-between bg-gray-50 dark:bg-white/[0.03] px-3 py-1.5 border-b border-gray-200 dark:border-lerd-border"
  >
    <span class="text-[10px] font-semibold text-gray-400 uppercase tracking-wider">{label}</span>
    <button
      onclick={copy}
      class="text-[10px] font-medium text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 transition-colors"
    >
      {#if copied}
        <span class="text-emerald-600 dark:text-emerald-500">Copied!</span>
      {:else}
        Copy
      {/if}
    </button>
  </div>
  <pre
    class="bg-gray-50 dark:bg-black/40 text-gray-600 dark:text-gray-400 px-3 py-2.5 text-[10px] leading-relaxed overflow-x-auto whitespace-pre"
  >{text}</pre>
</div>
