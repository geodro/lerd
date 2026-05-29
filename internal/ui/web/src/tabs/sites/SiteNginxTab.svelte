<script lang="ts">
  import NginxEditor from '$components/NginxEditor.svelte';
  import ConfigToolbar from '$components/ConfigToolbar.svelte';
  import {
    getSiteNginx,
    loadSiteNginxBackups,
    loadSiteNginxBackupContent,
    type Site,
    type SiteNginxBackup
  } from '$stores/sites';
  import { openNginxSaveModal, openNginxRestoreModal, openNginxResetModal } from '$stores/modals';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
    /** Domain whose override to edit. Defaults to the site's primary domain;
     *  pass a worktree's domain to edit that worktree's override instead. */
    domain?: string;
    /** Called after a successful save so the host can close the editor. */
    onSaved?: () => void;
  }
  let { site, domain, onSaved = () => {} }: Props = $props();

  let original = $state<string>('');
  let text = $state<string>('');
  let path = $state<string>('');
  let exists = $state<boolean>(false);
  let loading = $state(true);
  // `error` is the fatal load-time error that replaces the editor pane.
  // Anything raised AFTER a successful initial load (restore, reset, save
  // refresh) writes to `actionError` instead so the editor pane stays
  // visible and the user can keep editing without losing their buffer.
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

  // The domain is the only reactive input that should re-load the editor.
  // Every site mutation in lerd-ui triggers a sites WebSocket broadcast,
  // so the parent passes a fresh site object reference on every push
  // (even when the domain is unchanged). Reading site.domain inside the
  // effect would re-fire on each push and clobber unsaved edits; pinning
  // to a $derived(string) lets Svelte short-circuit those false triggers.
  const currentDomain = $derived(domain || site.domain);

  // Reload content + backups whenever the selected domain changes. The
  // domain guard in the resolver drops stale responses if the user
  // switches sites mid-fetch. Backup-listing errors are stored in a
  // dedicated `backupsError` so a transient 500 doesn't blank the editor
  // — the override content is still loadable independently.
  $effect(() => {
    const domain = currentDomain;
    loading = true;
    error = '';
    actionError = '';
    backupsError = '';
    original = '';
    text = '';
    path = '';
    backups = [];
    Promise.all([getSiteNginx(domain), loadSiteNginxBackups(domain)])
      .then(([res, listRes]) => {
        if (currentDomain !== domain) return;
        original = res.content;
        text = res.content;
        path = res.path;
        exists = res.exists;
        if (listRes.ok) {
          backups = listRes.list;
        } else {
          backupsError = listRes.error || 'Could not load backups';
        }
      })
      .catch((e: unknown) => {
        if (currentDomain !== domain) return;
        error = e instanceof Error ? e.message : String(e);
      })
      .finally(() => {
        if (currentDomain === domain) loading = false;
      });
  });

  // Drop the pending copy-feedback timer on unmount so a 1.5s-late
  // firing doesn't try to mutate $state after the component is gone.
  $effect(() => {
    return () => {
      if (copyTimer) clearTimeout(copyTimer);
    };
  });

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

  // refreshAfterAction reloads the editor state after a successful save /
  // restore / reset. It is wrapped in try/catch so a transient backend
  // failure during the refresh surfaces as an inline `actionError` rather
  // than blanking the editor pane via the load-time `error` slot (which
  // would wipe the user's visible context).
  async function refreshAfterAction(domain: string) {
    try {
      const [res, listRes] = await Promise.all([
        getSiteNginx(domain),
        loadSiteNginxBackups(domain)
      ]);
      if (currentDomain !== domain) return;
      original = res.content;
      text = res.content;
      path = res.path;
      exists = res.exists;
      if (listRes.ok) {
        backups = listRes.list;
        backupsError = '';
      } else {
        backupsError = listRes.error || 'Could not load backups';
      }
    } catch (e: unknown) {
      if (currentDomain !== domain) return;
      actionError = e instanceof Error ? e.message : String(e);
    }
  }

  // restore is only reachable when a backup file exists — the button hides
  // itself otherwise. It loads the backup, opens the diff modal, and on
  // accept points both the buffer and the on-screen "original" baseline at
  // the restored bytes so the dirty indicator clears cleanly. The diff
  // baseline is `original` (the on-disk saved content) rather than `text`
  // (the in-buffer content possibly carrying unsaved edits) so the diff
  // the user previews exactly matches what the backend will write.
  async function restore() {
    if (!latestBackup) return;
    restoring = true;
    actionError = '';
    try {
      const restoredDomain = currentDomain;
      const restoredName = latestBackup.name;
      const backupContent = await loadSiteNginxBackupContent(restoredDomain, restoredName);
      openNginxRestoreModal(
        {
          domain: restoredDomain,
          current: original,
          backupName: restoredName,
          backup: backupContent
        },
        async () => {
          if (currentDomain !== restoredDomain) return;
          original = backupContent;
          text = backupContent;
          exists = true;
          const listRes = await loadSiteNginxBackups(restoredDomain);
          if (currentDomain !== restoredDomain) return;
          if (listRes.ok) {
            backups = listRes.list;
            backupsError = '';
          } else {
            backupsError = listRes.error || 'Could not load backups';
          }
        }
      );
    } catch (e: unknown) {
      // Inline error so the editor pane stays visible; the user can keep
      // their buffer intact and try again or pick a different backup.
      actionError = e instanceof Error ? e.message : String(e);
    } finally {
      restoring = false;
    }
  }

  // revert is a pure buffer-level action: it discards the user's edits and
  // re-aligns the editor with whatever was on disk (or the seeded template
  // if no file existed). The saved file is never touched.
  function revert() {
    text = original;
  }

  // reset deletes the saved override file on disk and reloads nginx. The
  // confirm modal makes this a deliberate two-click action because it's
  // disk-destructive (backups are preserved in custom.d.bkp/ but the live
  // override is gone). On success we re-fetch so the editor returns to the
  // bundled-template state with exists=false.
  function reset() {
    const resetDomain = currentDomain;
    openNginxResetModal({ domain: resetDomain, path }, () => refreshAfterAction(resetDomain));
  }

  function save() {
    const savedDomain = currentDomain;
    // onSuccess fires only on a SUCCESSFUL save; on validation failure the
    // confirm modal stays open with the nginx -t diagnostic and the editor's
    // text is untouched. On success we close the editor entirely — reopening
    // remounts and reloads the saved file from disk, so there's no stale
    // dirty/backup state to refresh.
    openNginxSaveModal(
      { domain: savedDomain, content: text, original, exists },
      () => onSaved()
    );
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
