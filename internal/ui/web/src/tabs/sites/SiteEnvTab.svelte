<script lang="ts">
  import EnvBlock from '$components/EnvBlock.svelte';
  import { loadSiteEnv, type Site } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
    branch: string;
  }
  let { site, branch }: Props = $props();

  let text = $state<string>('');
  let loading = $state(true);
  let error = $state<string>('');

  const envPath = $derived.by(() => {
    if (branch) {
      const wt = (site.worktrees || []).find((w) => w.branch === branch);
      if (wt?.path) return wt.path + '/.env';
    }
    return (site.path || '') + '/.env';
  });

  $effect(() => {
    const domain = site.domain;
    const b = branch;
    loading = true;
    error = '';
    text = '';
    loadSiteEnv(domain, b)
      .then((t) => {
        text = t;
      })
      .catch((e: unknown) => {
        error = e instanceof Error ? e.message : String(e);
      })
      .finally(() => {
        loading = false;
      });
  });
</script>

<div class="overflow-y-auto">
  {#if loading}
    <p class="text-xs text-gray-400">{m.common_loading()}</p>
  {:else if error}
    <p class="text-xs text-red-500 dark:text-red-400">{error}</p>
  {:else if text === ''}
    <p class="text-xs text-gray-400">
      {m.sites_env_missing({ path: envPath })}
    </p>
  {:else}
    <EnvBlock {text} label=".env" />
  {/if}
</div>
