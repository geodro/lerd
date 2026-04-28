<script lang="ts">
  import { goToTab } from '$stores/route';

  interface Props {
    domain: string;
  }
  let { domain }: Props = $props();

  function open() {
    goToTab('sites', domain);
  }
</script>

<button
  onclick={open}
  class="inline-flex items-center gap-1.5 mt-1 text-xs font-medium bg-gray-100 dark:bg-white/5 hover:bg-gray-200 dark:hover:bg-white/10 border border-gray-200 dark:border-lerd-border text-gray-700 dark:text-gray-300 rounded-full px-2 py-0.5 transition-colors"
>
  <span class="w-1.5 h-1.5 rounded-full shrink-0 bg-gray-400"></span>
  {domain}
</button>
