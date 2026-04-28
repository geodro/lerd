<script lang="ts">
  interface Props {
    label: string;
    value: string;
    mono?: boolean;
  }
  let { label, value, mono = true }: Props = $props();
</script>

<div class="flex items-center justify-between">
  <span class="text-sm text-gray-500">{label}</span>
  {#if mono}
    <code
      class="bg-gray-100 dark:bg-white/5 border border-gray-200 dark:border-lerd-border px-1.5 py-0.5 rounded text-gray-600 dark:text-gray-400 text-xs"
    >{value}</code>
  {:else}
    <span class="text-sm text-gray-600 dark:text-gray-400">{value}</span>
  {/if}
</div>
