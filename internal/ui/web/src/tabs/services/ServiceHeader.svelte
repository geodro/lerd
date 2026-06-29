<script lang="ts">
  import { onDestroy, type Snippet } from 'svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import ButtonMenu, { type ButtonMenuAction } from '$components/ButtonMenu.svelte';
  import ServiceDependencies from './ServiceDependencies.svelte';
  import ServiceDeleteModal from './ServiceDeleteModal.svelte';
  import ServiceReinstallModal from './ServiceReinstallModal.svelte';
  import ServicePortsModal from './ServicePortsModal.svelte';
  import {
    type Service,
    services as allServices,
    serviceLabel,
    detailLabel,
    isServiceWorker,
    serviceAction,
    streamServiceAction,
    checkServiceUpdates,
    updateProgress,
    loadServices
  } from '$stores/services';
  import { adminServiceFor } from '$stores/presetSuggestions';
  import { openDashboard } from '$stores/dashboard';
  import { m } from '../../paraglide/messages.js';

  function localDetailLabel(s: Service): string {
    if (s.queue_site) return m.services_labels_queueWorker();
    if (s.horizon_site) return m.services_labels_horizon();
    if (s.stripe_listener_site) return m.services_labels_stripeListener();
    if (s.schedule_worker_site) return m.services_labels_scheduler();
    if (s.reverb_site) return m.services_labels_reverb();
    if (s.worker_site && s.worker_name === 'vite') return m.services_labels_vite();
    if (s.worker_site && s.worker_name) return m.services_labels_worker({ name: s.worker_name });
    return detailLabel(s);
  }

  interface Props {
    svc: Service;
  }
  let { svc }: Props = $props();

  const admin = $derived(adminServiceFor(svc, $allServices));

  async function openAdmin() {
    if (!admin) return;
    if (admin.status !== 'active') {
      await serviceAction(admin.name, 'start');
      await loadServices();
    }
    const latest = $allServices.find((s) => s.name === admin.name) || admin;
    openDashboard(latest);
  }

  const isWorker = $derived(isServiceWorker(svc));
  const active = $derived(svc.status === 'active');
  // When active and a host port is exposed, the status pill shows the port (a
  // moved port reads at a glance) and copies 127.0.0.1:<port> on click; otherwise
  // it falls back to the status word.
  const exposedPort = $derived(active && svc.port ? svc.port : 0);
  const exposedAddr = $derived(exposedPort ? `127.0.0.1:${exposedPort}` : '');
  let portCopied = $state(false);
  let portCopyTimer: ReturnType<typeof setTimeout> | null = null;
  async function copyExposedAddr() {
    if (!exposedAddr) return;
    try {
      await navigator.clipboard.writeText(exposedAddr);
      portCopied = true;
      if (portCopyTimer) clearTimeout(portCopyTimer);
      portCopyTimer = setTimeout(() => (portCopied = false), 1500);
    } catch {
      /* clipboard unavailable; nothing to recover */
    }
  }
  const pillLabel = $derived(exposedPort ? String(exposedPort) : svc.status);
  const pillTitle = $derived(
    exposedPort
      ? portCopied
        ? m.common_copied()
        : m.services_portPillTitle({ addr: exposedAddr })
      : ''
  );
  const portConflicts = $derived(
    !active && svc.port_conflicts && svc.port_conflicts.length > 0 ? svc.port_conflicts : []
  );
  const portConflictTitle = $derived(
    portConflicts.length > 0
      ? m.services_portConflictTitle({ ports: portConflicts.map((c) => c.port).join(', ') })
      : ''
  );

  let localBusy = $state(false);
  let deleteOpen = $state(false);
  let reinstallOpen = $state(false);
  let portsOpen = $state(false);
  let checking = $state(false);
  let checkMessage = $state<{ text: string; tone: 'ok' | 'info' | 'error' } | null>(null);
  let checkMessageTimer: ReturnType<typeof setTimeout> | null = null;
  const updating = $derived($updateProgress[svc.name]);
  const busy = $derived(localBusy || checking || Boolean(updating));

  function setCheckMessage(text: string, tone: 'ok' | 'info' | 'error') {
    if (checkMessageTimer) clearTimeout(checkMessageTimer);
    checkMessage = { text, tone };
    checkMessageTimer = setTimeout(() => (checkMessage = null), 4000);
  }

  onDestroy(() => {
    if (checkMessageTimer) clearTimeout(checkMessageTimer);
  });

  async function runCheckUpdates() {
    checking = true;
    try {
      const res = await checkServiceUpdates(svc.name);
      if (!res.ok) {
        setCheckMessage(m.services_checkUpdatesFailed(), 'error');
        return;
      }
      const tag = res.avail?.latest_tag || res.avail?.upgrade_tag || '';
      if (res.avail?.available && tag) {
        setCheckMessage(m.services_checkUpdatesFound({ tag }), 'info');
      } else {
        setCheckMessage(m.services_checkUpdatesUpToDate(), 'ok');
      }
    } finally {
      checking = false;
    }
  }

  async function run(action: Parameters<typeof serviceAction>[1]) {
    localBusy = true;
    try {
      await serviceAction(svc.name, action);
    } finally {
      localBusy = false;
    }
  }

  async function confirmDelete(opts: { removeData: boolean }) {
    localBusy = true;
    try {
      await serviceAction(svc.name, 'remove', { removeData: opts.removeData });
    } finally {
      localBusy = false;
    }
  }

  async function confirmReinstall(opts: { resetData: boolean }) {
    await streamServiceAction(svc.name, 'reinstall', { resetData: opts.resetData });
  }

  async function runUpdate(tag?: string) {
    await streamServiceAction(svc.name, 'update', tag ? { tag } : {});
  }
  async function runMigrate(tag: string) {
    await streamServiceAction(svc.name, 'migrate', { tag });
  }
  async function runRollback() {
    await streamServiceAction(svc.name, 'rollback');
  }

  function rollbackTagFromImage(image: string | undefined): string {
    if (!image) return '';
    const at = image.lastIndexOf(':');
    return at > 0 ? image.slice(at + 1) : image;
  }

  function buildActions(icons: {
    external: Snippet;
    start: Snippet;
    stop: Snippet;
    restart: Snippet;
    update: Snippet;
    upgrade: Snippet;
    migrate: Snippet;
    rollback: Snippet;
    pin: Snippet;
    trash: Snippet;
    checkUpdates: Snippet;
  }): ButtonMenuAction[] {
    const lifecycle: ButtonMenuAction[] = [];
    const rest: ButtonMenuAction[] = [];

    if (svc.update_available || updating) {
      const tag = svc.latest_version || '';
      lifecycle.push({
        id: 'update',
        tone: 'success',
        icon: icons.update,
        label: tag ? m.services_updateTo({ tag }) : m.services_update(),
        title: tag ? m.services_updateAvailableTo({ tag }) : m.services_updateAvailable(),
        onclick: () => runUpdate()
      });
    }

    if (svc.upgrade_version && svc.migration_supported === false && !updating) {
      const tag = svc.upgrade_version;
      rest.push({
        id: 'upgrade',
        tone: 'warn',
        icon: icons.upgrade,
        label: m.services_upgradeTo({ tag }),
        title: m.services_upgradeWarning({ tag }),
        onclick: () => runUpdate(tag)
      });
    }

    if (svc.upgrade_version && svc.migration_supported === true && !updating) {
      const tag = svc.upgrade_version;
      rest.push({
        id: 'migrate',
        tone: 'info',
        icon: icons.migrate,
        label: m.services_migrateTo({ tag }),
        title: m.services_migrateExplain({ tag }),
        onclick: () => runMigrate(tag)
      });
    }

    if (active && admin) {
      const adminLabel = m.services_openAdmin({ name: serviceLabel(admin.name) });
      rest.push({
        id: 'admin',
        tone: 'info',
        icon: icons.external,
        label: adminLabel,
        title: adminLabel,
        onclick: openAdmin
      });
    } else if (active && svc.dashboard) {
      rest.push({
        id: 'dashboard',
        icon: icons.external,
        label: m.services_dashboard(),
        title: m.services_dashboard(),
        onclick: () => openDashboard(svc)
      });
    } else if (active && svc.connection_url) {
      rest.push({
        id: 'connection',
        icon: icons.external,
        label: m.services_openConnection(),
        title: svc.connection_url,
        href: svc.connection_url
      });
    }

    if (!isWorker && !active && !updating) {
      rest.push({
        id: 'start',
        tone: 'primary',
        icon: icons.start,
        label: m.common_start(),
        onclick: () => run('start')
      });
    }

    if (active && !isWorker) {
      rest.push({
        id: 'restart',
        icon: icons.restart,
        label: m.common_restart(),
        title: m.sites_restartContainer(),
        onclick: () => run('restart')
      });
    }

    if (active) {
      rest.push({
        id: 'stop',
        icon: icons.stop,
        label: m.common_stop(),
        onclick: () => run('stop')
      });
    }

    if (!isWorker) {
      rest.push({
        id: 'pin',
        tone: svc.pinned ? 'warn' : 'secondary',
        icon: icons.pin,
        label: svc.pinned ? m.services_pinned() : m.services_pin(),
        title: svc.pinned ? m.services_unpinTitle() : m.services_pinTitle(),
        onclick: () => run(svc.pinned ? 'unpin' : 'pin')
      });
    }

    if (!isWorker && !updating) {
      rest.push({
        id: 'check-updates',
        tone: 'secondary',
        icon: icons.checkUpdates,
        label: checking ? m.services_checkUpdatesChecking() : m.services_checkUpdates(),
        title: m.services_checkUpdatesTitle(),
        onclick: runCheckUpdates
      });
    }

    if (!isWorker && !updating) {
      rest.push({
        id: 'reinstall',
        tone: 'secondary',
        icon: icons.restart,
        label: m.services_reinstall_action(),
        title: m.services_reinstall_menuTitle(),
        onclick: () => (reinstallOpen = true)
      });
    }

    if (svc.previous_version && svc.can_rollback !== false && !updating) {
      const tag = rollbackTagFromImage(svc.previous_version);
      rest.push({
        id: 'rollback',
        tone: 'secondary',
        icon: icons.rollback,
        label: m.services_rollbackTo({ tag }),
        title: m.services_rollbackExplain({ tag }),
        onclick: () => runRollback()
      });
    }

    if (!isWorker) {
      const removeLabel = svc.is_default ? m.common_remove() : m.services_removeCustom();
      rest.push({
        id: 'remove',
        tone: 'danger',
        icon: icons.trash,
        label: removeLabel,
        title: removeLabel,
        onclick: () => (deleteOpen = true)
      });
    }

    return [...lifecycle, ...rest];
  }
</script>

<div
  class="flex flex-wrap items-center justify-between gap-y-2 px-3 py-2.5 border-b border-gray-100 dark:border-lerd-border shrink-0"
>
  <div class="flex items-center gap-3">
    <div>
      <div class="flex items-center gap-2">
        <span class="font-semibold text-gray-900 dark:text-white text-base">{localDetailLabel(svc)}</span>
        {#if svc.version && !isWorker}
          <span class="text-xs font-normal tabular-nums text-gray-500 dark:text-gray-400">{svc.version}</span>
        {/if}
        <StatusPill
          tone={active ? 'ok' : 'muted'}
          label={pillLabel}
          title={pillTitle}
          onclick={exposedPort ? copyExposedAddr : undefined}
        />
        {#if portConflicts.length > 0}
          <span
            class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium bg-amber-50 dark:bg-amber-500/10 text-amber-700 dark:text-amber-300 border border-amber-200 dark:border-amber-500/30"
            title={portConflictTitle}
            role="status"
            aria-label={portConflictTitle}
          >
            <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
              <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
              <line x1="12" y1="9" x2="12" y2="13"/>
              <line x1="12" y1="17" x2="12.01" y2="17"/>
            </svg>
            <span>port {portConflicts.map((c) => c.port).join(', ')} in use</span>
          </span>
        {/if}
      </div>

      {#if svc.depends_on && svc.depends_on.length > 0}
        <ServiceDependencies names={svc.depends_on} />
      {/if}
    </div>
  </div>

  {#snippet startIcon()}
    <svg class="w-3.5 h-3.5" fill="currentColor" viewBox="0 0 24 24">
      <path d="M8 5v14l11-7z"/>
    </svg>
  {/snippet}
  {#snippet stopIcon()}
    <svg class="w-3.5 h-3.5" fill="currentColor" viewBox="0 0 24 24">
      <rect x="6" y="6" width="12" height="12" rx="1"/>
    </svg>
  {/snippet}
  {#snippet restartIcon()}
    <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"/>
    </svg>
  {/snippet}
  {#snippet updateIcon()}
    <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">
      <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
      <polyline points="17 8 12 3 7 8"/>
      <line x1="12" y1="3" x2="12" y2="15"/>
    </svg>
  {/snippet}
  {#snippet upgradeIcon()}
    <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">
      <path d="M12 9v4M12 17h.01"/>
      <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
    </svg>
  {/snippet}
  {#snippet migrateIcon()}
    <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">
      <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/>
      <polyline points="3.27 6.96 12 12.01 20.73 6.96"/>
      <line x1="12" y1="22.08" x2="12" y2="12"/>
    </svg>
  {/snippet}
  {#snippet rollbackIcon()}
    <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">
      <path d="M3 12a9 9 0 1 0 3-6.7"/>
      <polyline points="3 4 3 10 9 10"/>
    </svg>
  {/snippet}
  {#snippet pinIcon()}
    {#if svc.pinned}
      <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="currentColor">
        <path d="M12 17v5M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V17h14v-1.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V6h1a2 2 0 0 0 0-4H8a2 2 0 0 0 0 4h1v4.76z"/>
      </svg>
    {:else}
      <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <line x1="12" y1="17" x2="12" y2="22"/>
        <path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V17h14v-1.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V6h1a2 2 0 0 0 0-4H8a2 2 0 0 0 0 4h1v4.76z"/>
      </svg>
    {/if}
  {/snippet}
  {#snippet trashIcon()}
    <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/>
    </svg>
  {/snippet}
  {#snippet externalIcon()}
    <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14"/>
    </svg>
  {/snippet}
  {#snippet checkUpdatesIcon()}
    <svg
      class={`w-3.5 h-3.5 ${checking ? 'animate-spin' : ''}`}
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
      viewBox="0 0 24 24"
    >
      <path d="M21 12a9 9 0 1 1-3-6.7"/>
      <polyline points="21 4 21 10 15 10"/>
    </svg>
  {/snippet}

  <div class="flex flex-col items-end gap-1.5">
    <ButtonMenu
      actions={buildActions({
        external: externalIcon,
        start: startIcon,
        stop: stopIcon,
        restart: restartIcon,
        update: updateIcon,
        upgrade: upgradeIcon,
        migrate: migrateIcon,
        rollback: rollbackIcon,
        pin: pinIcon,
        trash: trashIcon,
        checkUpdates: checkUpdatesIcon
      })}
      {busy}
      onSettings={isWorker ? undefined : () => (portsOpen = true)}
      settingsTitle={m.services_ports_settingsTitle()}
    />
    {#if updating}
      <span
        class="text-[11px] text-gray-500 dark:text-gray-400 truncate max-w-[32ch]"
        title={updating.message}
      >{updating.message}</span>
    {:else if checkMessage}
      <span
        class={'text-[11px] truncate max-w-[32ch] ' + (
          checkMessage.tone === 'error'
            ? 'text-rose-600 dark:text-rose-400'
            : checkMessage.tone === 'info'
              ? 'text-emerald-600 dark:text-emerald-400'
              : 'text-gray-500 dark:text-gray-400'
        )}
        title={checkMessage.text}
      >{checkMessage.text}</span>
    {:else if svc.update_available && svc.latest_version}
      <span class="text-[11px] text-emerald-600 dark:text-emerald-400 truncate max-w-[32ch]">
        {m.system_lerd_available({ version: svc.latest_version })}
      </span>
    {/if}
  </div>
</div>

<ServiceDeleteModal
  open={deleteOpen}
  {svc}
  onclose={() => (deleteOpen = false)}
  onconfirm={confirmDelete}
/>

<ServiceReinstallModal
  open={reinstallOpen}
  {svc}
  onclose={() => (reinstallOpen = false)}
  onconfirm={confirmReinstall}
/>

<ServicePortsModal open={portsOpen} {svc} onclose={() => (portsOpen = false)} />
