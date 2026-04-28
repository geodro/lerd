<script lang="ts">
  import EnvBlock from '$components/EnvBlock.svelte';
  import type { Service } from '$stores/services';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    svc: Service;
  }
  let { svc }: Props = $props();
</script>

<div class="p-3 sm:p-5 space-y-3 overflow-y-auto">
  {#if svc.connection_url}
    <div class="rounded-lg border border-gray-200 dark:border-lerd-border overflow-hidden">
      <div
        class="flex items-center justify-between bg-gray-50 dark:bg-white/[0.03] px-3 py-1.5 border-b border-gray-200 dark:border-lerd-border"
      >
        <span class="text-[10px] font-semibold text-gray-400 uppercase tracking-wider">{m.services_env_connect()}</span>
      </div>
      <div class="bg-gray-50 dark:bg-black/40 px-3 py-2.5">
        <a href={svc.connection_url} class="font-mono text-[10px] text-sky-600 dark:text-sky-400 hover:underline break-all">{svc.connection_url}</a>
        <p class="text-[10px] text-gray-400 dark:text-gray-600 mt-1.5">
          {@html m.services_env_connectHint({ loopback4: '<code class="text-gray-500 dark:text-gray-400">127.0.0.1</code>', loopback6: '<code class="text-gray-500 dark:text-gray-400">localhost</code>' })}
        </p>
      </div>
    </div>
  {/if}
  {#if svc.env_vars && Object.keys(svc.env_vars).length > 0}
    <EnvBlock vars={svc.env_vars} />
  {:else}
    <p class="text-xs text-gray-400">{m.services_env_none()}</p>
  {/if}
</div>
