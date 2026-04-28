<script lang="ts">
  import { version } from '$stores/version';
  import { goToTab } from '$stores/route';
  import { m } from '../paraglide/messages.js';
</script>

<button
  class="mb-4 flex items-center justify-center relative"
  title={$version.hasUpdate ? m.system_lerd_updateAvailable() : 'Lerd'}
  onclick={() => goToTab('system', 'lerd')}
>
  <img src="/icons/icon.svg" class="w-7 h-7 rounded-lg" alt="Lerd" />
  {#if $version.hasUpdate}
    <span
      class="absolute -top-0.5 -right-0.5 w-2.5 h-2.5 rounded-full bg-yellow-400 ring-2 ring-white dark:ring-lerd-card"
    ></span>
  {/if}
</button>
