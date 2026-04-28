<script lang="ts">
  import Toggle from '$components/Toggle.svelte';
  import {
    type Site,
    setSiteVersion,
    toggleTLS,
    toggleLANShare,
    toggleQueue,
    toggleHorizon,
    toggleSchedule,
    toggleReverb,
    toggleWorker,
    loadSites
  } from '$stores/sites';
  import { loadServices } from '$stores/services';
  import { phpVersions } from '$stores/phpVersions';
  import { nodeVersions } from '$stores/nodeVersions';
  import { status } from '$stores/status';
  import LANShareLink from './LANShareLink.svelte';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  let tlsBusy = $state(false);
  let lanBusy = $state(false);
  let queueBusy = $state(false);
  let horizonBusy = $state(false);
  let scheduleBusy = $state(false);
  let reverbBusy = $state(false);
  let versionBusy = $state(false);
  const workerBusy = $state<Record<string, boolean>>({});

  async function runAction(
    setBusy: (b: boolean) => void,
    action: () => Promise<{ ok: boolean; error?: string }>
  ) {
    setBusy(true);
    try {
      await action();
      await Promise.all([loadSites(), loadServices()]);
    } finally {
      setBusy(false);
    }
  }

  async function onPhpChange(e: Event) {
    const v = (e.target as HTMLSelectElement).value;
    versionBusy = true;
    try {
      const r = await setSiteVersion(site, 'php', v);
      if (!r.ok) alert(m.sites_controls_versionChangeFailed({ error: r.error || '' }));
      await loadSites();
    } finally {
      versionBusy = false;
    }
  }

  async function onNodeChange(e: Event) {
    const v = (e.target as HTMLSelectElement).value;
    versionBusy = true;
    try {
      const r = await setSiteVersion(site, 'node', v);
      if (!r.ok) alert(m.sites_controls_versionChangeFailed({ error: r.error || '' }));
      await loadSites();
    } finally {
      versionBusy = false;
    }
  }
</script>

<div class="px-3 sm:px-5 py-3 border-b border-gray-100 dark:border-lerd-border shrink-0">
  <div class="flex items-center gap-4 overflow-x-auto">
    {#if site.custom_container}
      <span class="text-xs text-violet-500 dark:text-violet-400 border border-violet-200 dark:border-violet-500/30 rounded px-2 py-1">
        {(site.container_image || 'container') + ' :' + site.container_port}
      </span>
    {:else if $phpVersions.length > 0}
      <select
        value={site.php_version ?? ''}
        onchange={onPhpChange}
        disabled={versionBusy}
        class="text-xs bg-transparent border border-gray-200 dark:border-lerd-border rounded px-2 py-1 text-gray-700 dark:text-gray-300 hover:border-gray-300 dark:hover:border-lerd-muted focus:outline-none focus:border-lerd-red/50 disabled:opacity-50 cursor-pointer transition-colors"
      >
        <option value="" disabled>{m.sites_controls_phpPlaceholder()}</option>
        {#each $phpVersions as v (v)}<option value={v}>PHP {v}</option>{/each}
      </select>
    {:else}
      <span class="text-xs text-gray-400 border border-gray-200 dark:border-lerd-border rounded px-2 py-1 opacity-50">PHP ...</span>
    {/if}

    {#if $status.node_managed_by_lerd && $nodeVersions.length > 0}
      <select
        value={site.node_version ?? ''}
        onchange={onNodeChange}
        disabled={versionBusy}
        class="text-xs bg-transparent border border-gray-200 dark:border-lerd-border rounded px-2 py-1 text-gray-700 dark:text-gray-300 hover:border-gray-300 dark:hover:border-lerd-muted focus:outline-none focus:border-lerd-red/50 disabled:opacity-50 cursor-pointer transition-colors"
      >
        <option value="">{m.sites_controls_nodeDefault()}</option>
        {#each $nodeVersions as v (v)}<option value={v}>Node {v}</option>{/each}
      </select>
    {/if}

    <div class="flex items-center gap-1.5">
      <Toggle
        on={Boolean(site.tls)}
        loading={tlsBusy}
        onclick={() => runAction((b) => (tlsBusy = b), () => toggleTLS(site))}
        title={site.tls ? m.sites_controls_httpsToggle_on() : m.sites_controls_httpsToggle_off()}
      />
      <span class="text-xs text-gray-500 dark:text-gray-400">{m.sites_controls_https()}</span>
    </div>

    <div class="flex items-center gap-1.5">
      <Toggle
        on={Boolean(site.lan_port)}
        tone="teal"
        loading={lanBusy}
        onclick={() => runAction((b) => (lanBusy = b), () => toggleLANShare(site))}
        title={site.lan_port ? m.sites_controls_lanToggle_on() : m.sites_controls_lanToggle_off()}
      />
      <span class="text-xs text-gray-500 dark:text-gray-400">{m.sites_controls_lan()}</span>
      {#if site.lan_share_url}
        <LANShareLink domain={site.domain} url={site.lan_share_url} />
      {/if}
    </div>

    <div class="w-px h-4 bg-gray-200 dark:bg-lerd-border mx-0.5"></div>

    {#if site.has_queue_worker}
      <div class="flex items-center gap-1.5">
        <Toggle
          on={Boolean(site.queue_running)}
          failing={Boolean(site.queue_failing)}
          tone="amber"
          loading={queueBusy}
          onclick={() => runAction((b) => (queueBusy = b), () => toggleQueue(site))}
          title={site.queue_failing ? m.sites_controls_queueToggle_failing() : site.queue_running ? m.sites_controls_queueToggle_on() : m.sites_controls_queueToggle_off()}
        />
        <span class="text-xs text-gray-500 dark:text-gray-400">{m.sites_controls_queue()}</span>
      </div>
    {/if}

    {#if site.has_horizon}
      <div class="flex items-center gap-1.5">
        <Toggle
          on={Boolean(site.horizon_running)}
          failing={Boolean(site.horizon_failing)}
          tone="amber"
          loading={horizonBusy}
          onclick={() => runAction((b) => (horizonBusy = b), () => toggleHorizon(site))}
          title={site.horizon_failing ? m.sites_controls_horizonToggle_failing() : site.horizon_running ? m.sites_controls_horizonToggle_on() : m.sites_controls_horizonToggle_off()}
        />
        <span class="text-xs text-gray-500 dark:text-gray-400">{m.sites_controls_horizon()}</span>
      </div>
    {/if}

    {#if site.has_schedule_worker}
      <div class="flex items-center gap-1.5">
        <Toggle
          on={Boolean(site.schedule_running)}
          failing={Boolean(site.schedule_failing)}
          tone="emerald"
          loading={scheduleBusy}
          onclick={() => runAction((b) => (scheduleBusy = b), () => toggleSchedule(site))}
          title={site.schedule_running ? m.sites_controls_scheduleToggle_on() : m.sites_controls_scheduleToggle_off()}
        />
        <span class="text-xs text-gray-500 dark:text-gray-400">{m.sites_controls_schedule()}</span>
      </div>
    {/if}

    {#if site.has_reverb}
      <div class="flex items-center gap-1.5">
        <Toggle
          on={Boolean(site.reverb_running)}
          failing={Boolean(site.reverb_failing)}
          tone="sky"
          loading={reverbBusy}
          onclick={() => runAction((b) => (reverbBusy = b), () => toggleReverb(site))}
          title={site.reverb_running ? m.sites_controls_reverbToggle_on() : m.sites_controls_reverbToggle_off()}
        />
        <span class="text-xs text-gray-500 dark:text-gray-400">{m.sites_controls_reverb()}</span>
      </div>
    {/if}

    {#each site.framework_workers || [] as w (w.name)}
      <div class="flex items-center gap-1.5">
        <Toggle
          on={Boolean(w.running)}
          failing={Boolean(w.failing)}
          tone="indigo"
          loading={Boolean(workerBusy[w.name])}
          onclick={() => runAction((b) => (workerBusy[w.name] = b), () => toggleWorker(site, w))}
          title={w.running ? 'Stop ' + (w.label || w.name) : 'Start ' + (w.label || w.name)}
        />
        <span class="text-xs text-gray-500 dark:text-gray-400">{w.label || w.name}</span>
      </div>
    {/each}
  </div>
</div>
