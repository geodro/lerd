<script lang="ts">
  import DetailPanel from '$components/DetailPanel.svelte';
  import SiteHeader from './SiteHeader.svelte';
  import SiteControls from './SiteControls.svelte';
  import SiteLogs from './SiteLogs.svelte';
  import SiteTinkerTab from './SiteTinkerTab.svelte';
  import DumpsTab from '$tabs/DumpsTab.svelte';
  import type { Site } from '$stores/sites';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  type TabId = 'overview' | 'tinker' | 'dumps';
  const TAB_STORAGE_KEY = 'lerd:siteDetailTab';

  function readStoredTab(): TabId {
    if (typeof localStorage === 'undefined') return 'overview';
    const v = localStorage.getItem(TAB_STORAGE_KEY);
    if (v === 'tinker' || v === 'dumps') return v;
    return 'overview';
  }

  let active = $state<TabId>(readStoredTab());
  let activeWorktreeBranch = $state<string>('');
  const canTinker = $derived(Boolean(site.php_version));

  $effect(() => {
    if (active === 'tinker' && !canTinker) active = 'overview';
  });

  $effect(() => {
    if (typeof localStorage !== 'undefined') {
      localStorage.setItem(TAB_STORAGE_KEY, active);
    }
  });

  // Reset selection when the site changes or the chosen branch disappears.
  $effect(() => {
    if (!activeWorktreeBranch) return;
    const exists = (site.worktrees || []).some((w) => w.branch === activeWorktreeBranch);
    if (!exists) activeWorktreeBranch = '';
  });

  const tabBtn = (tab: TabId, isActive: boolean) =>
    'pb-1 text-xs font-medium border-b-2 transition-colors ' +
    (isActive
      ? 'border-lerd-red text-lerd-red'
      : 'border-transparent text-gray-500 hover:text-gray-700 dark:hover:text-gray-300');
</script>

{#snippet tabs()}
  <button class={tabBtn('overview', active === 'overview')} onclick={() => (active = 'overview')}>Overview</button>
  {#if canTinker}
    <button class={tabBtn('tinker', active === 'tinker')} onclick={() => (active = 'tinker')}>Tinker</button>
  {/if}
  <button class={tabBtn('dumps', active === 'dumps')} onclick={() => (active = 'dumps')}>Dumps</button>
{/snippet}

<DetailPanel>
  <SiteHeader
    {site}
    {tabs}
    {activeWorktreeBranch}
    onWorktreeChange={(b) => (activeWorktreeBranch = b)}
  />
  {#if active === 'overview'}
    {#if !site.paused}
      <SiteControls {site} {activeWorktreeBranch} />
    {/if}
    <SiteLogs {site} {activeWorktreeBranch} />
  {:else if active === 'tinker'}
    {#key site.domain + '@' + activeWorktreeBranch}
      <SiteTinkerTab {site} branch={activeWorktreeBranch} />
    {/key}
  {:else if active === 'dumps'}
    <DumpsTab siteScope={site.name} />
  {/if}
</DetailPanel>
