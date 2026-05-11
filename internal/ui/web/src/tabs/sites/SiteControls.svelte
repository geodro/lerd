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
    toggleStripe,
    toggleWorker,
    setWorktreeDBIsolated,
    loadSites
  } from '$stores/sites';
  import { loadServices } from '$stores/services';
  import { phpVersions } from '$stores/phpVersions';
  import { nodeVersions } from '$stores/nodeVersions';
  import { status } from '$stores/status';
  import LANShareLink from './LANShareLink.svelte';
  import WorktreeDBIsolateModal from './WorktreeDBIsolateModal.svelte';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
    activeWorktreeBranch?: string;
  }
  let { site, activeWorktreeBranch = '' }: Props = $props();

  const activeWorktree = $derived.by(() => {
    if (!activeWorktreeBranch) return undefined;
    return (site.worktrees || []).find((w) => w.branch === activeWorktreeBranch);
  });
  const effectivePhp = $derived(activeWorktree?.php_version ?? site.php_version ?? '');
  const effectiveNode = $derived(activeWorktree?.node_version ?? site.node_version ?? '');
  const phpInherited = $derived(Boolean(activeWorktree) && !activeWorktree?.php_version_override);
  const nodeInherited = $derived(Boolean(activeWorktree) && !activeWorktree?.node_version_override);
  const dbCapable = $derived((site.services || []).some((s) => /^(mysql|mariadb|postgres)/.test(s)));
  const dbIsolated = $derived(Boolean(activeWorktree?.db_isolated));
  let dbBusy = $state(false);

  let isolateModalOpen = $state(false);

  function onDBIsolatedChange() {
    if (!activeWorktreeBranch || dbBusy) return;
    if (dbIsolated) {
      void disableIsolation();
    } else {
      isolateModalOpen = true;
    }
  }

  async function disableIsolation() {
    if (!confirm('Drop the isolated database for this worktree? Migrations applied here will be lost.')) {
      return;
    }
    dbBusy = true;
    try {
      const res = await setWorktreeDBIsolated(site.domain, activeWorktreeBranch, false);
      if (!res.ok) alert('Failed to toggle worktree DB: ' + (res.error || ''));
      await loadSites();
    } finally {
      dbBusy = false;
    }
  }

  async function enableIsolation(source: string) {
    dbBusy = true;
    try {
      const res = await setWorktreeDBIsolated(site.domain, activeWorktreeBranch, true, source);
      if (!res.ok) alert('Failed to isolate worktree DB: ' + (res.error || ''));
      await loadSites();
    } finally {
      dbBusy = false;
    }
  }

  let tlsBusy = $state(false);
  let lanBusy = $state(false);
  let versionBusy = $state(false);

  // Worker toggles need a different busy semantic than the other toggles:
  // the API returns when the unit is asked to start, but the actual
  // running/failing state in `site` only catches up after AfterUnitChange's
  // PollNow goroutine completes and the WS push lands. Without this delay,
  // the toggle re-enables in its old position for a beat before the new
  // state arrives, looking broken. We track the desired post-action value
  // per worker and keep the toggle loading+disabled until site state
  // matches it (or 15s elapses, in case the unit failed to come up).
  type Pending = { desired: boolean; timeoutId: ReturnType<typeof setTimeout> };
  const pending = $state<Record<string, Pending | undefined>>({});

  function isPending(key: string): boolean {
    return pending[key] !== undefined;
  }

  function startTransition(key: string, desired: boolean): boolean {
    if (pending[key]) return false;
    const timeoutId = setTimeout(() => {
      delete pending[key];
    }, 15000);
    pending[key] = { desired, timeoutId };
    return true;
  }

  function clearTransition(key: string) {
    const p = pending[key];
    if (!p) return;
    clearTimeout(p.timeoutId);
    delete pending[key];
  }

  function reconcile(key: string, actual: boolean) {
    const p = pending[key];
    if (p && p.desired === actual) clearTransition(key);
  }

  // Reconcile every time site state changes (from WS push or loadSites).
  $effect(() => {
    reconcile('queue', Boolean(site.queue_running));
    reconcile('horizon', Boolean(site.horizon_running));
    reconcile('schedule', Boolean(site.schedule_running));
    reconcile('reverb', Boolean(site.reverb_running));
    reconcile('stripe', Boolean(site.stripe_running));
    const sourceWorkers = activeWorktreeBranch
      ? activeWorktree?.framework_workers || []
      : site.framework_workers || [];
    for (const w of sourceWorkers) {
      reconcile('worker:' + w.name, Boolean(w.running));
    }
  });

  async function transition(
    key: string,
    desired: boolean,
    action: () => Promise<{ ok: boolean; error?: string }>
  ) {
    if (!startTransition(key, desired)) return;
    const r = await action();
    // Server refused — clear immediately so the user can retry. Successful
    // calls leave the transition in flight; the $effect above clears it
    // when site state catches up.
    if (!r.ok) {
      clearTransition(key);
      return;
    }
    // Kick a refresh so we pick up the change even if the WS push is late.
    await Promise.all([loadSites(), loadServices()]);
  }

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
      const r = await setSiteVersion(site, 'php', v, activeWorktreeBranch);
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
      const r = await setSiteVersion(site, 'node', v, activeWorktreeBranch);
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
        value={effectivePhp}
        onchange={onPhpChange}
        disabled={versionBusy}
        title={phpInherited ? 'Inherits from main' : ''}
        class="text-xs bg-white dark:bg-lerd-bg border rounded px-2 py-1 text-gray-700 dark:text-gray-300 hover:border-gray-300 dark:hover:border-lerd-muted focus:outline-none focus:border-lerd-red/50 disabled:opacity-50 cursor-pointer transition-colors {phpInherited ? 'border-dashed border-violet-300 dark:border-violet-700' : 'border-gray-200 dark:border-lerd-border'}"
      >
        <option value="" disabled class="bg-white text-gray-700 dark:bg-lerd-bg dark:text-gray-300">{m.sites_controls_phpPlaceholder()}</option>
        {#each $phpVersions as v (v)}<option value={v} class="bg-white text-gray-700 dark:bg-lerd-bg dark:text-gray-300">PHP {v}{activeWorktreeBranch && v === effectivePhp && phpInherited ? ' (inherited)' : ''}</option>{/each}
      </select>
    {:else}
      <span class="text-xs text-gray-400 border border-gray-200 dark:border-lerd-border rounded px-2 py-1 opacity-50">PHP ...</span>
    {/if}

    {#if $status.node_managed_by_lerd && $nodeVersions.length > 0}
      <select
        value={effectiveNode}
        onchange={onNodeChange}
        disabled={versionBusy}
        title={nodeInherited ? 'Inherits from main' : ''}
        class="text-xs bg-white dark:bg-lerd-bg border rounded px-2 py-1 text-gray-700 dark:text-gray-300 hover:border-gray-300 dark:hover:border-lerd-muted focus:outline-none focus:border-lerd-red/50 disabled:opacity-50 cursor-pointer transition-colors {nodeInherited ? 'border-dashed border-violet-300 dark:border-violet-700' : 'border-gray-200 dark:border-lerd-border'}"
      >
        <option value="" class="bg-white text-gray-700 dark:bg-lerd-bg dark:text-gray-300">{m.sites_controls_nodeDefault()}</option>
        {#each $nodeVersions as v (v)}<option value={v} class="bg-white text-gray-700 dark:bg-lerd-bg dark:text-gray-300">Node {v}{activeWorktreeBranch && v === effectiveNode && nodeInherited ? ' (inherited)' : ''}</option>{/each}
      </select>
    {/if}

    {#if $status.dns?.enabled !== false}
      <div class="flex items-center gap-1.5">
        <Toggle
          on={Boolean(site.tls)}
          loading={tlsBusy}
          onclick={() => runAction((b) => (tlsBusy = b), () => toggleTLS(site))}
          title={site.tls ? m.sites_controls_httpsToggle_on() : m.sites_controls_httpsToggle_off()}
        />
        <span class="text-xs text-gray-500 dark:text-gray-400">{m.sites_controls_https()}</span>
      </div>
    {/if}

    {#if activeWorktreeBranch && dbCapable}
      <div class="flex items-center gap-1.5" title={dbIsolated ? `Worktree DB: ${activeWorktree?.db_database ?? ''}` : 'Share parent database'}>
        <Toggle
          on={dbIsolated}
          tone="teal"
          loading={dbBusy}
          onclick={onDBIsolatedChange}
        />
        <span class="text-xs text-gray-500 dark:text-gray-400">Isolated DB</span>
      </div>
    {/if}

    {#snippet lanShare()}
      {@const isWT = Boolean(activeWorktreeBranch)}
      {@const lanPort = isWT ? activeWorktree?.lan_port ?? 0 : site.lan_port ?? 0}
      {@const lanURL = isWT ? activeWorktree?.lan_share_url ?? '' : site.lan_share_url ?? ''}
      {@const lanDomain = isWT ? (activeWorktree?.domain ?? site.domain) : site.domain}
      <div class="flex items-center gap-1.5">
        <Toggle
          on={Boolean(lanPort)}
          tone="teal"
          loading={lanBusy}
          onclick={() => runAction((b) => (lanBusy = b), () => toggleLANShare(site, activeWorktreeBranch))}
          title={lanPort ? m.sites_controls_lanToggle_on() : m.sites_controls_lanToggle_off()}
        />
        <span class="text-xs text-gray-500 dark:text-gray-400">{m.sites_controls_lan()}</span>
        {#if lanURL}
          <LANShareLink domain={lanDomain} url={lanURL} siteDomain={site.domain} branch={activeWorktreeBranch} />
        {/if}
      </div>
    {/snippet}
    {@render lanShare()}

    <div class="w-px h-4 bg-gray-200 dark:bg-lerd-border mx-0.5"></div>

    {#if activeWorktreeBranch}
      {@const wtWorkers = activeWorktree?.framework_workers || []}
      {#if wtWorkers.length === 0}
        <span
          class="text-[11px] text-gray-400 dark:text-gray-500 italic"
          title={m.sites_controls_workersFromMainTitle()}
        >
          {m.sites_controls_workersFromMain()}
        </span>
      {:else}
        {#each wtWorkers as w (w.name)}
          <div class="flex items-center gap-1.5">
            <Toggle
              on={Boolean(w.running)}
              failing={Boolean(w.failing)}
              tone="indigo"
              loading={isPending('worker:' + w.name)}
              disabled={isPending('worker:' + w.name)}
              onclick={() => transition('worker:' + w.name, !w.running, () => toggleWorker(site, w, activeWorktreeBranch))}
              title={w.running ? 'Stop ' + (w.label || w.name) : 'Start ' + (w.label || w.name)}
            />
            <span class="text-xs text-gray-500 dark:text-gray-400">{w.label || w.name}</span>
          </div>
        {/each}
      {/if}
    {:else}
      {#if site.has_queue_worker}
        <div class="flex items-center gap-1.5">
          <Toggle
            on={Boolean(site.queue_running)}
            failing={Boolean(site.queue_failing)}
            tone="amber"
            loading={isPending('queue')}
            disabled={isPending('queue')}
            onclick={() => transition('queue', !site.queue_running, () => toggleQueue(site))}
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
            loading={isPending('horizon')}
            disabled={isPending('horizon')}
            onclick={() => transition('horizon', !site.horizon_running, () => toggleHorizon(site))}
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
            loading={isPending('schedule')}
            disabled={isPending('schedule')}
            onclick={() => transition('schedule', !site.schedule_running, () => toggleSchedule(site))}
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
            loading={isPending('reverb')}
            disabled={isPending('reverb')}
            onclick={() => transition('reverb', !site.reverb_running, () => toggleReverb(site))}
            title={site.reverb_running ? m.sites_controls_reverbToggle_on() : m.sites_controls_reverbToggle_off()}
          />
          <span class="text-xs text-gray-500 dark:text-gray-400">{m.sites_controls_reverb()}</span>
        </div>
      {/if}

      {#if site.stripe_secret_set}
        <div class="flex items-center gap-1.5">
          <Toggle
            on={Boolean(site.stripe_running)}
            tone="violet"
            loading={isPending('stripe')}
            disabled={isPending('stripe')}
            onclick={() => transition('stripe', !site.stripe_running, () => toggleStripe(site))}
            title={site.stripe_running ? m.sites_controls_stripeToggle_on() : m.sites_controls_stripeToggle_off()}
          />
          <span class="text-xs text-gray-500 dark:text-gray-400">{m.sites_controls_stripe()}</span>
        </div>
      {/if}

      {#each site.framework_workers || [] as w (w.name)}
        <div class="flex items-center gap-1.5">
          <Toggle
            on={Boolean(w.running)}
            failing={Boolean(w.failing)}
            tone="indigo"
            loading={isPending('worker:' + w.name)}
            disabled={isPending('worker:' + w.name)}
            onclick={() => transition('worker:' + w.name, !w.running, () => toggleWorker(site, w))}
            title={w.running ? 'Stop ' + (w.label || w.name) : 'Start ' + (w.label || w.name)}
          />
          <span class="text-xs text-gray-500 dark:text-gray-400">{w.label || w.name}</span>
        </div>
      {/each}
    {/if}
  </div>
</div>

{#if activeWorktreeBranch}
  <WorktreeDBIsolateModal
    open={isolateModalOpen}
    {site}
    branch={activeWorktreeBranch}
    onclose={() => (isolateModalOpen = false)}
    onconfirm={(source) => {
      isolateModalOpen = false;
      void enableIsolation(source);
    }}
  />
{/if}
