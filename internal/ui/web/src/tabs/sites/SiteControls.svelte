<script lang="ts">
  import Toggle from '$components/Toggle.svelte';
  import {
    type Site,
    setSiteVersion,
    toggleQueue,
    toggleHorizon,
    setHorizonReload,
    setOctaneReload,
    toggleSchedule,
    toggleReverb,
    toggleStripe,
    setStripeConfig,
    toggleWorker,
    setWorktreeDBIsolated,
    loadSites
  } from '$stores/sites';
  import { loadServices } from '$stores/services';
  import { phpVersions, phpOptionsForSite } from '$stores/phpVersions';
  import { nodeVersions } from '$stores/nodeVersions';
  import { status } from '$stores/status';
  import WorktreeDBIsolateModal from './WorktreeDBIsolateModal.svelte';
  import HorizonControl from './HorizonControl.svelte';
  import HorizonReloadWatcherModal from './HorizonReloadWatcherModal.svelte';
  import OctaneControl from './OctaneControl.svelte';
  import OctaneReloadWatcherModal from './OctaneReloadWatcherModal.svelte';
  import StripeControl from './StripeControl.svelte';
  import CommandsDropdown from '$components/CommandsDropdown.svelte';
  import SiteDoctorModal from './SiteDoctorModal.svelte';
  import Dropdown from '$components/Dropdown.svelte';
  import ToggleButton from '$components/ToggleButton.svelte';
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
  const phpOptions = $derived(
    phpOptionsForSite(site.runtime, $phpVersions, $status.frankenphp_php_versions, effectivePhp)
  );
  // When host bun is available, the Node dropdown offers a "bun" entry that
  // pins .lerd.yaml js_runtime (project-level), leaving node_version intact.
  // js_runtime is project-level, so the bun toggle only belongs on the main
  // site dropdown — offering it per worktree would let a worktree action flip
  // the whole project's runtime and show a confusing selection.
  const usingBun = $derived(!activeWorktreeBranch && site.js_runtime === 'bun');
  // Bake "Node " into the version labels so the bun entry can stay bare "bun"
  // instead of reading "Node bun" (the Dropdown prefixes its label onto rows).
  const nodeOptions = $derived([
    ...$nodeVersions.map((v) => ({ value: v, label: 'Node ' + v })),
    ...(!activeWorktreeBranch && $status.bun_available
      ? [{ value: 'bun', label: 'bun', description: 'JS runtime' }]
      : [])
  ]);
  const nodeValue = $derived(usingBun ? 'bun' : effectiveNode);
  const dbCapable = $derived((site.services || []).some((s) => /^(mysql|mariadb|postgres)/.test(s)));
  const dbIsolated = $derived(Boolean(activeWorktree?.db_isolated));
  let dbBusy = $state(false);
  let isolateModalOpen = $state(false);

  // Laravel Doctor lives behind an on-demand button next to Commands rather than
  // a permanent tab: its checks (including a migrate:status exec) only run when
  // the modal is opened, so a healthy site carries no extra weight.
  const canDoctor = $derived(Boolean(site.is_laravel));
  let doctorOpen = $state(false);

  function onDBIsolatedChange() {
    if (!activeWorktreeBranch || dbBusy) return;
    if (dbIsolated) {
      void disableIsolation();
    } else {
      isolateModalOpen = true;
    }
  }

  async function disableIsolation() {
    if (!confirm(m.sites_controls_dbDropConfirm())) {
      return;
    }
    dbBusy = true;
    try {
      const res = await setWorktreeDBIsolated(site.domain, activeWorktreeBranch, false);
      if (!res.ok) alert(m.sites_controls_dbToggleFailed({ error: res.error || '' }));
      await loadSites();
    } finally {
      dbBusy = false;
    }
  }

  async function enableIsolation(source: string) {
    dbBusy = true;
    try {
      const res = await setWorktreeDBIsolated(site.domain, activeWorktreeBranch, true, source);
      if (!res.ok) alert(m.sites_controls_dbIsolateFailed({ error: res.error || '' }));
      await loadSites();
    } finally {
      dbBusy = false;
    }
  }

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

  let watcherModalOpen = $state(false);
  // Held for the whole reload toggle, which restarts Horizon under the hood.
  // Horizon's running flag dips false mid-restart; without this the segment
  // would flicker to "off". We keep it in its loading-dot state instead, the
  // same as a worker that's starting, until the restart settles.
  let reloadRestarting = $state(false);

  // Flip Horizon auto-reload and ride out the restart. Any failure surfaces so
  // a click always gives feedback rather than silently reverting.
  async function applyReload(desired: boolean) {
    if (reloadRestarting) return;
    reloadRestarting = true;
    try {
      const r = await setHorizonReload(site, desired);
      if (!r.ok) {
        alert(m.sites_controls_horizonReloadFailed({ error: r.error || '' }));
        return;
      }
      await Promise.all([loadSites(), loadServices()]);
    } finally {
      // Always release the guard, even on a thrown rejection, so the toggle
      // can't deadlock in its loading state.
      reloadRestarting = false;
    }
  }

  // Enabling needs the chokidar watcher. When it's missing (Vite 8 no longer
  // ships it) we open a modal offering to install it rather than letting the
  // toggle silently refuse. Disabling, and enabling when chokidar is already
  // present, go straight through.
  async function onToggleHorizonReload() {
    const desired = !site.horizon_reload;
    if (desired && !site.horizon_reload_ready) {
      watcherModalOpen = true;
      return;
    }
    await applyReload(desired);
  }

  // Octane (FrankenPHP worker mode) auto-reload, mirroring the Horizon flow:
  // re-applying the FrankenPHP container restarts it, so the control dips and
  // settles the same way the Horizon segment does.
  let octaneWatcherModalOpen = $state(false);
  let octaneReloadRestarting = $state(false);

  async function applyOctaneReload(desired: boolean) {
    if (octaneReloadRestarting) return;
    octaneReloadRestarting = true;
    try {
      const r = await setOctaneReload(site, desired);
      if (!r.ok) {
        alert(m.sites_controls_octaneReloadFailed({ error: r.error || '' }));
        return;
      }
      await Promise.all([loadSites(), loadServices()]);
    } finally {
      octaneReloadRestarting = false;
    }
  }

  async function onToggleOctaneReload() {
    const desired = !site.octane_reload;
    if (desired && !site.octane_reload_ready) {
      octaneWatcherModalOpen = true;
      return;
    }
    await applyOctaneReload(desired);
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

<div class="px-3 pt-3 shrink-0">
  <div class="flex items-center gap-3">
  <div class="flex items-center gap-3 overflow-x-auto flex-1 min-w-0">
    {#if site.custom_container}
      <span class="text-xs text-violet-500 dark:text-violet-400 border border-violet-200 dark:border-violet-500/30 rounded-sm px-2 py-1">
        {(site.container_image || 'container') + ' :' + site.container_port}
      </span>
    {:else if site.host_proxy}
      <span class="text-xs text-violet-500 dark:text-violet-400 border border-violet-200 dark:border-violet-500/30 rounded-sm px-2 py-1">
        {m.sites_controls_proxyBadge()}{site.host_port ? ' :' + site.host_port : ''}
      </span>
    {:else if site.runtime === 'fpm-custom'}
      <span class="text-xs text-violet-500 dark:text-violet-400 border border-violet-200 dark:border-violet-500/30 rounded-sm px-2 py-1">
        PHP {effectivePhp} · custom image
      </span>
    {:else if site.uses_php}
      {#if phpOptions.length > 0}
        <Dropdown
          label="PHP"
          value={effectivePhp}
          options={phpOptions}
          disabled={versionBusy}
          inherited={phpInherited}
          inheritedSuffix={m.sites_controls_inheritedSuffix()}
          title={phpInherited ? m.sites_controls_inheritsFromMain() : ''}
          placeholder={m.sites_controls_phpPlaceholder()}
          onchange={(v) => onPhpChange({ target: { value: v } } as unknown as Event)}
        />
      {:else}
        <span class="text-xs text-gray-400 border border-gray-200 dark:border-lerd-border rounded-sm px-2 py-1 opacity-50">PHP ...</span>
      {/if}
    {/if}

    {#if $status.node_managed_by_lerd && $nodeVersions.length > 0}
      <Dropdown
        value={nodeValue}
        options={nodeOptions}
        disabled={versionBusy}
        inherited={nodeInherited && !usingBun}
        inheritedSuffix={m.sites_controls_inheritedSuffix()}
        title={nodeInherited ? m.sites_controls_inheritsFromMain() : ''}
        placeholder={$status.node_default ? m.sites_controls_nodeDefaultVersion({ version: $status.node_default }) : m.sites_controls_nodeDefault()}
        onchange={(v) => onNodeChange({ target: { value: v } } as unknown as Event)}
      />
    {/if}

    {#if activeWorktreeBranch && dbCapable}
      <ToggleButton
        label={m.sites_controls_dbIsolated()}
        on={dbIsolated}
        loading={dbBusy}
        onclick={onDBIsolatedChange}
        title={dbIsolated ? m.sites_controls_dbIsolatedTitle({ db: activeWorktree?.db_database ?? '' }) : m.sites_controls_dbShareParent()}
      />
    {/if}

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
          <ToggleButton
            label={w.label || w.name}
            on={Boolean(w.running)}
            failing={Boolean(w.failing)}
            loading={isPending('worker:' + w.name)}
            disabled={isPending('worker:' + w.name)}
            onclick={() => transition('worker:' + w.name, !w.running, () => toggleWorker(site, w, activeWorktreeBranch))}
            title={w.running ? m.sites_controls_workerToggle_on({ label: w.label || w.name }) : m.sites_controls_workerToggle_off({ label: w.label || w.name })}
          />
        {/each}
      {/if}
    {:else}
      {#if site.has_queue_worker}
        <ToggleButton
          label={m.sites_controls_queue()}
          on={Boolean(site.queue_running)}
          failing={Boolean(site.queue_failing)}
          loading={isPending('queue')}
          disabled={isPending('queue')}
          onclick={() => transition('queue', !site.queue_running, () => toggleQueue(site))}
          title={site.queue_failing ? m.sites_controls_queueToggle_failing() : site.queue_running ? m.sites_controls_queueToggle_on() : m.sites_controls_queueToggle_off()}
        />
      {/if}

      {#if site.has_horizon}
        <HorizonControl
          running={Boolean(site.horizon_running)}
          failing={Boolean(site.horizon_failing)}
          reload={Boolean(site.horizon_reload)}
          horizonLoading={isPending('horizon') || reloadRestarting}
          reloadLoading={reloadRestarting}
          onToggle={() => transition('horizon', !site.horizon_running, () => toggleHorizon(site))}
          onToggleReload={onToggleHorizonReload}
        />
      {/if}

      {#if site.runtime === 'frankenphp' && site.runtime_worker && site.is_laravel}
        <OctaneControl
          reload={Boolean(site.octane_reload)}
          reloadLoading={octaneReloadRestarting}
          onToggleReload={onToggleOctaneReload}
        />
      {/if}

      {#if site.has_schedule_worker}
        <ToggleButton
          label={m.sites_controls_schedule()}
          on={Boolean(site.schedule_running)}
          failing={Boolean(site.schedule_failing)}
          loading={isPending('schedule')}
          disabled={isPending('schedule')}
          onclick={() => transition('schedule', !site.schedule_running, () => toggleSchedule(site))}
          title={site.schedule_running ? m.sites_controls_scheduleToggle_on() : m.sites_controls_scheduleToggle_off()}
        />
      {/if}

      {#if site.has_reverb}
        <ToggleButton
          label={m.sites_controls_reverb()}
          on={Boolean(site.reverb_running)}
          failing={Boolean(site.reverb_failing)}
          loading={isPending('reverb')}
          disabled={isPending('reverb')}
          onclick={() => transition('reverb', !site.reverb_running, () => toggleReverb(site))}
          title={site.reverb_running ? m.sites_controls_reverbToggle_on() : m.sites_controls_reverbToggle_off()}
        />
      {/if}

      {#if site.stripe_secret_set}
        <StripeControl
          running={Boolean(site.stripe_running)}
          loading={isPending('stripe')}
          webhookPath={site.stripe_webhook_path}
          onToggle={() => transition('stripe', !site.stripe_running, () => toggleStripe(site))}
          onSaveConfig={(path) => setStripeConfig(site, path)}
        />
      {/if}

      {#each site.framework_workers || [] as w (w.name)}
        {@const isVite = w.name === 'vite'}
        {@const shortLabel = isVite ? m.sites_controls_vite() : w.label || w.name}
        <ToggleButton
          label={shortLabel}
          on={Boolean(w.running)}
          failing={Boolean(w.failing)}
          loading={isPending('worker:' + w.name)}
          disabled={isPending('worker:' + w.name)}
          onclick={() => transition('worker:' + w.name, !w.running, () => toggleWorker(site, w))}
          title={isVite
            ? w.running
              ? m.sites_controls_viteToggle_on()
              : m.sites_controls_viteToggle_off()
            : w.running
              ? 'Stop ' + shortLabel
              : 'Start ' + shortLabel}
        />
      {/each}
    {/if}
  </div>

    {#if canDoctor}
      <button
        type="button"
        onclick={() => (doctorOpen = true)}
        class="shrink-0 inline-flex items-center justify-center h-7 w-7 rounded-md border border-gray-200 dark:border-lerd-border bg-white dark:bg-lerd-card hover:border-lerd-red hover:text-lerd-red transition-colors text-gray-700 dark:text-gray-200"
        title={m.sites_doctor_title()}
        aria-label={m.sites_doctor_button()}
      >
        <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M4.8 2.3A.3.3 0 1 0 5 2H4a2 2 0 00-2 2v5a6 6 0 006 6 6 6 0 006-6V4a2 2 0 00-2-2h-1a.3.3 0 10.2.3" />
          <path d="M8 15v1a6 6 0 006 6 6 6 0 006-6v-4" />
          <circle cx="20" cy="10" r="2" />
        </svg>
      </button>
    {/if}

    <CommandsDropdown domain={site.domain} branch={activeWorktreeBranch} />
  </div>
</div>

{#if canDoctor}
  <SiteDoctorModal
    open={doctorOpen}
    {site}
    branch={activeWorktreeBranch}
    onclose={() => (doctorOpen = false)}
  />
{/if}

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

<HorizonReloadWatcherModal
  open={watcherModalOpen}
  {site}
  onclose={() => (watcherModalOpen = false)}
  oninstalled={() => void applyReload(true)}
/>

<OctaneReloadWatcherModal
  open={octaneWatcherModalOpen}
  {site}
  onclose={() => (octaneWatcherModalOpen = false)}
  oninstalled={() => void applyOctaneReload(true)}
/>
