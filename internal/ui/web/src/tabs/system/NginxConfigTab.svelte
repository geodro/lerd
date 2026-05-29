<script lang="ts">
  import NginxEditor from '$components/NginxEditor.svelte';
  import ConfigToolbar from '$components/ConfigToolbar.svelte';
  import {
    getNginxConfig,
    loadNginxConfigBackups,
    loadNginxConfigBackupContent
  } from '$stores/nginx';
  import type { SiteNginxBackup } from '$stores/sites';
  import {
    openNginxGlobalSaveModal,
    openNginxGlobalRestoreModal,
    openNginxGlobalResetModal
  } from '$stores/modals';
  import { onMount } from 'svelte';
  import { m } from '../../paraglide/messages.js';

  let original = $state<string>('');
  let text = $state<string>('');
  let path = $state<string>('');
  let exists = $state<boolean>(false);
  let loading = $state(true);
  let error = $state<string>('');
  let actionError = $state<string>('');
  let backupsError = $state<string>('');
  let copied = $state(false);
  let copyTimer: ReturnType<typeof setTimeout> | null = null;
  let backups = $state<SiteNginxBackup[]>([]);
  let restoring = $state(false);

  const dirty = $derived(text !== original);
  const latestBackup = $derived(backups[0]);
  const hasBackup = $derived(backups.length > 0 && !loading && !error);
  const canRevert = $derived(dirty && !loading && !error);
  const canReset = $derived(exists && !loading && !error);
  const canSave = $derived(dirty && !loading && !error);

  async function load() {
    loading = true;
    error = '';
    actionError = '';
    backupsError = '';
    original = '';
    text = '';
    path = '';
    backups = [];
    // allSettled so a transient failure on the config GET doesn't discard a
    // successful backups list (the user would otherwise lose Restore access).
    const [cfgRes, listRes] = await Promise.allSettled([getNginxConfig(), loadNginxConfigBackups()]);
    if (cfgRes.status === 'fulfilled') {
      original = cfgRes.value.content;
      text = cfgRes.value.content;
      path = cfgRes.value.path;
      exists = cfgRes.value.exists;
    } else {
      error = cfgRes.reason instanceof Error ? cfgRes.reason.message : String(cfgRes.reason);
    }
    if (listRes.status === 'fulfilled') {
      if (listRes.value.ok) {
        backups = listRes.value.list;
      } else {
        backupsError = listRes.value.error || 'Could not load backups';
      }
    } else {
      backupsError = listRes.reason instanceof Error ? listRes.reason.message : String(listRes.reason);
    }
    loading = false;
  }

  onMount(() => {
    void load();
    return () => {
      if (copyTimer) clearTimeout(copyTimer);
    };
  });

  async function refreshAfterAction() {
    const [cfgRes, listRes] = await Promise.allSettled([getNginxConfig(), loadNginxConfigBackups()]);
    if (cfgRes.status === 'fulfilled') {
      original = cfgRes.value.content;
      text = cfgRes.value.content;
      path = cfgRes.value.path;
      exists = cfgRes.value.exists;
    } else {
      actionError = cfgRes.reason instanceof Error ? cfgRes.reason.message : String(cfgRes.reason);
    }
    if (listRes.status === 'fulfilled') {
      if (listRes.value.ok) {
        backups = listRes.value.list;
        backupsError = '';
      } else {
        backupsError = listRes.value.error || 'Could not load backups';
      }
    } else {
      backupsError = listRes.reason instanceof Error ? listRes.reason.message : String(listRes.reason);
    }
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
      copied = true;
      if (copyTimer) clearTimeout(copyTimer);
      copyTimer = setTimeout(() => (copied = false), 1500);
    } catch {
      /* no-op */
    }
  }

  async function restore() {
    if (!latestBackup) return;
    restoring = true;
    actionError = '';
    try {
      const name = latestBackup.name;
      const backupContent = await loadNginxConfigBackupContent(name);
      openNginxGlobalRestoreModal(
        {
          current: original,
          backupName: name,
          backup: backupContent
        },
        async () => {
          // The modal's outer try/catch swallows whatever this throws (the
          // comment there points at refreshAfterAction, which we don't use
          // here). Catch locally so a failed backups reload still surfaces
          // as actionError rather than silently going stale.
          original = backupContent;
          text = backupContent;
          exists = true;
          try {
            const listRes = await loadNginxConfigBackups();
            if (listRes.ok) {
              backups = listRes.list;
              backupsError = '';
            } else {
              backupsError = listRes.error || 'Could not load backups';
            }
          } catch (e: unknown) {
            actionError = e instanceof Error ? e.message : String(e);
          }
        }
      );
    } catch (e: unknown) {
      actionError = e instanceof Error ? e.message : String(e);
    } finally {
      restoring = false;
    }
  }

  function revert() {
    text = original;
  }

  function reset() {
    openNginxGlobalResetModal({ path }, () => refreshAfterAction());
  }

  function save() {
    openNginxGlobalSaveModal({ content: text, original, exists }, () => refreshAfterAction());
  }
</script>

<div class="flex flex-col h-full">
  <ConfigToolbar
    {path}
    {dirty}
    {loading}
    {error}
    backupCount={backups.length}
    latestBackupName={latestBackup?.name}
    {backupsError}
    {actionError}
    {canRevert}
    {canReset}
    {canSave}
    {hasBackup}
    {restoring}
    {copied}
    onCopy={copy}
    onRevert={revert}
    onReset={reset}
    onRestore={restore}
    onSave={save}
  />

  <div class="flex-1 overflow-hidden bg-gray-50 dark:bg-black/40">
    {#if loading}
      <p class="text-xs text-gray-400 px-3 py-2.5">{m.common_loading()}</p>
    {:else if error}
      <p class="text-xs text-red-500 dark:text-red-400 px-3 py-2.5">{error}</p>
    {:else}
      <div class="h-full min-h-64">
        <NginxEditor bind:value={text} />
      </div>
    {/if}
  </div>
</div>
