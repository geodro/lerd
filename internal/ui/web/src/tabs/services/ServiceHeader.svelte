<script lang="ts">
  import type { Snippet } from 'svelte';
  import StatusPill from '$components/StatusPill.svelte';
  import ButtonMenu, { type ButtonMenuAction } from '$components/ButtonMenu.svelte';
  import ParentSiteBadge from './ParentSiteBadge.svelte';
  import ServiceDependencies from './ServiceDependencies.svelte';
  import { goToTab } from '$stores/route';
  import {
    type Service,
    services as allServices,
    serviceLabel,
    detailLabel,
    isServiceWorker,
    parentSiteDomain,
    serviceAction,
    streamServiceAction,
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
  const parent = $derived(parentSiteDomain(svc));

  let localBusy = $state(false);
  const updating = $derived($updateProgress[svc.name]);
  const busy = $derived(localBusy || Boolean(updating));

  async function run(action: Parameters<typeof serviceAction>[1]) {
    localBusy = true;
    try {
      await serviceAction(svc.name, action);
    } finally {
      localBusy = false;
    }
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

  function openSite(domain: string) {
    goToTab('sites', domain);
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
  }): ButtonMenuAction[] {
    const list: ButtonMenuAction[] = [];

    if (active && admin) {
      const adminLabel = m.services_openAdmin({ name: serviceLabel(admin.name) });
      list.push({
        id: 'admin',
        tone: 'info',
        icon: icons.external,
        label: adminLabel,
        title: adminLabel,
        onclick: openAdmin
      });
    } else if (active && svc.dashboard) {
      list.push({
        id: 'dashboard',
        icon: icons.external,
        label: m.services_dashboard(),
        title: m.services_dashboard(),
        onclick: () => openDashboard(svc)
      });
    } else if (active && svc.connection_url) {
      list.push({
        id: 'connection',
        icon: icons.external,
        label: m.services_openConnection(),
        title: svc.connection_url,
        href: svc.connection_url
      });
    }

    if (!isWorker && !active && !updating) {
      list.push({
        id: 'start',
        tone: 'primary',
        icon: icons.start,
        label: m.common_start(),
        onclick: () => run('start')
      });
    }

    if (svc.update_available || updating) {
      const tag = svc.latest_version || '';
      list.push({
        id: 'update',
        tone: 'success',
        icon: icons.update,
        label: m.services_update(),
        title: tag ? m.services_updateTo({ tag }) : m.services_update(),
        onclick: () => runUpdate()
      });
    }

    if (svc.upgrade_version && !svc.migration_supported && !updating) {
      const tag = svc.upgrade_version;
      list.push({
        id: 'upgrade',
        tone: 'warn',
        icon: icons.upgrade,
        label: m.services_upgradeTo({ tag }),
        title: m.services_upgradeWarning({ tag }),
        onclick: () => runUpdate(tag)
      });
    }

    if (svc.upgrade_version && svc.migration_supported && !updating) {
      const tag = svc.upgrade_version;
      list.push({
        id: 'migrate',
        tone: 'info',
        icon: icons.migrate,
        label: m.services_migrateTo({ tag }),
        title: m.services_migrateExplain({ tag }),
        onclick: () => runMigrate(tag)
      });
    }

    if (svc.previous_version && !updating) {
      const tag = rollbackTagFromImage(svc.previous_version);
      list.push({
        id: 'rollback',
        tone: 'secondary',
        icon: icons.rollback,
        label: m.services_rollbackTo({ tag }),
        title: m.services_rollbackExplain({ tag }),
        onclick: () => runRollback()
      });
    }

    if (active && !isWorker) {
      list.push({
        id: 'restart',
        icon: icons.restart,
        label: m.common_restart(),
        title: m.sites_restartContainer(),
        onclick: () => run('restart')
      });
    }

    if (active) {
      list.push({
        id: 'stop',
        icon: icons.stop,
        label: m.common_stop(),
        onclick: () => run('stop')
      });
    }

    if (!isWorker) {
      list.push({
        id: 'pin',
        tone: svc.pinned ? 'warn' : 'secondary',
        icon: icons.pin,
        label: svc.pinned ? m.services_pinned() : m.services_pin(),
        title: svc.pinned ? m.services_unpinTitle() : m.services_pinTitle(),
        onclick: () => run(svc.pinned ? 'unpin' : 'pin')
      });
    }

    if (!isWorker && svc.custom && !active) {
      list.push({
        id: 'remove',
        tone: 'danger',
        icon: icons.trash,
        label: m.services_removeCustom(),
        title: m.services_removeCustom(),
        onclick: () => run('remove')
      });
    }

    return list;
  }
</script>

<div
  class="flex flex-wrap items-center justify-between gap-y-2 px-3 sm:px-5 py-4 border-b border-gray-100 dark:border-lerd-border shrink-0"
>
  <div class="flex items-center gap-3">
    <div>
      <div class="flex items-center gap-2">
        <span class="font-semibold text-gray-900 dark:text-white text-base">{localDetailLabel(svc)}</span>
        {#if svc.version && !isWorker}
          <span class="text-xs font-normal tabular-nums text-gray-500 dark:text-gray-400">{svc.version}</span>
        {/if}
        <StatusPill tone={active ? 'ok' : 'muted'} label={svc.status} />
      </div>

      {#if parent}
        <ParentSiteBadge domain={parent} />
      {/if}

      {#if !isWorker && svc.site_domains && svc.site_domains.length > 0}
        <div class="flex flex-wrap gap-1 mt-1">
          {#each svc.site_domains as d (d)}
            <button
              onclick={() => openSite(d)}
              class="inline-flex items-center gap-1.5 text-xs font-medium bg-gray-100 dark:bg-white/5 hover:bg-gray-200 dark:hover:bg-white/10 border border-gray-200 dark:border-lerd-border text-gray-700 dark:text-gray-300 rounded-full px-2 py-0.5 transition-colors"
            >
              <span class="w-1.5 h-1.5 rounded-full shrink-0 bg-gray-400"></span>
              {d}
            </button>
          {/each}
        </div>
      {/if}

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

  <div class="flex items-center gap-2">
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
        trash: trashIcon
      })}
      {busy}
    />
    {#if updating}
      <span class="text-[11px] text-gray-500 dark:text-gray-400 ml-2 truncate max-w-[18ch]" title={updating.message}>{updating.message}</span>
    {/if}
  </div>
</div>
