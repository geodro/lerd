<script lang="ts">
  import TuningEditor from '$components/TuningEditor.svelte';
  import ConfigToolbar from '$components/ConfigToolbar.svelte';
  import {
    getPhpIni,
    loadPhpIniBackups,
    loadPhpIniBackupContent
  } from '$stores/phpVersions';
  import type { SiteNginxBackup } from '$stores/sites';
  import {
    openPhpIniSaveModal,
    openPhpIniRestoreModal,
    openPhpIniResetModal
  } from '$stores/modals';
  import { onMount } from 'svelte';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    version: string;
  }
  let { version }: Props = $props();

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

  // Pin the loader's reactive input to the version string so any unrelated
  // store push that re-renders the parent does not clobber unsaved edits.
  const currentVersion = $derived(version);

  $effect(() => {
    const v = currentVersion;
    loading = true;
    error = '';
    actionError = '';
    backupsError = '';
    original = '';
    text = '';
    path = '';
    backups = [];
    // allSettled so a transient failure on one half doesn't discard the
    // other half's data — e.g. backups loaded fine but getPhpIni 500s,
    // we still want the user able to Restore.
    Promise.allSettled([getPhpIni(v), loadPhpIniBackups(v)])
      .then(([cfgRes, listRes]) => {
        if (currentVersion !== v) return;
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
      })
      .finally(() => {
        if (currentVersion === v) loading = false;
      });
  });

  // Run cleanup on unmount. Plain onMount is cheaper than a $effect with no
  // reactive reads and makes the intent obvious.
  onMount(() => () => {
    if (copyTimer) clearTimeout(copyTimer);
  });

  async function refreshAfterAction(v: string) {
    try {
      const [cfgRes, listRes] = await Promise.allSettled([getPhpIni(v), loadPhpIniBackups(v)]);
      if (currentVersion !== v) return;
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
    } catch (e: unknown) {
      if (currentVersion !== v) return;
      actionError = e instanceof Error ? e.message : String(e);
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
      const v = currentVersion;
      const name = latestBackup.name;
      const backupContent = await loadPhpIniBackupContent(v, name);
      openPhpIniRestoreModal(
        { version: v, current: original, backupName: name, backup: backupContent },
        async () => {
          if (currentVersion !== v) return;
          original = backupContent;
          text = backupContent;
          exists = true;
          try {
            const listRes = await loadPhpIniBackups(v);
            if (currentVersion !== v) return;
            if (listRes.ok) {
              backups = listRes.list;
              backupsError = '';
            } else {
              backupsError = listRes.error || 'Could not load backups';
            }
          } catch (e: unknown) {
            if (currentVersion !== v) return;
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
    const v = currentVersion;
    openPhpIniResetModal({ version: v, path }, () => refreshAfterAction(v));
  }

  function save() {
    const v = currentVersion;
    openPhpIniSaveModal({ version: v, content: text, original, exists }, () => refreshAfterAction(v));
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
        <TuningEditor bind:value={text} />
      </div>
    {/if}
  </div>
</div>
