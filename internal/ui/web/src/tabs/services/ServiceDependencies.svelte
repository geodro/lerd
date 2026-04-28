<script lang="ts">
  import StatusDot from '$components/StatusDot.svelte';
  import { services } from '$stores/services';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    names: string[];
  }
  let { names }: Props = $props();

  const entries = $derived(
    names.map((n) => {
      const svc = $services.find((s) => s.name === n);
      return { name: n, active: svc?.status === 'active' };
    })
  );
</script>

<div class="flex items-center gap-1 flex-wrap mt-1">
  <span class="text-xs text-gray-400">{m.services_dependsOn()}</span>
  {#each entries as dep (dep.name)}
    <span
      class="inline-flex items-center gap-1 text-[11px] font-medium px-1.5 py-0.5 rounded bg-gray-100 dark:bg-white/5 text-gray-600 dark:text-gray-400 border border-gray-200 dark:border-lerd-border"
    >
      <StatusDot color={dep.active ? 'green' : 'gray'} size="xs" />
      {dep.name}
    </span>
  {/each}
</div>
