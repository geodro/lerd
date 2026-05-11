<script lang="ts">
  import { m } from '../paraglide/messages.js';
  interface Props {
    label?: string;
  }
  let { label = m.common_loading() }: Props = $props();
</script>

<div class="px-4 py-3 text-xs text-gray-400 dark:text-gray-500">{label}</div>
