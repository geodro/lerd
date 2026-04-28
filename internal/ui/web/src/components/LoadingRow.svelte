<script lang="ts">
  interface Props {
    label?: string;
  }
  let { label = 'Loading...' }: Props = $props();
</script>

<div class="px-4 py-3 text-xs text-gray-400 dark:text-gray-500">{label}</div>
