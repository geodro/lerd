<script lang="ts">
  import TuningEditor from '$components/TuningEditor.svelte';
  import {
    getServiceConfig,
    loadServiceTuningBackups,
    loadServiceTuningBackupContent,
    type Service,
    type ServiceTuningBackup
  } from '$stores/services';
  import {
    openTuningSaveModal,
    openTuningRestoreModal,
    openTuningResetModal
  } from '$stores/modals';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    svc: Service;
  }
  let { svc }: Props = $props();

  let original = $state<string>('');
  let text = $state<string>('');
  let target = $state<string>('');
  let exists = $state<boolean>(false);
  let loading = $state(true);
  // `error` is the fatal load-time error that replaces the editor pane.
  // Anything raised AFTER a successful initial load (restore fetch,
  // backups-list refresh) writes to `actionError` / `backupsError` so
  // the editor pane stays visible and the user can keep editing without
  // losing their buffer.
  let error = $state<string>('');
  let actionError = $state<string>('');
  let backupsError = $state<string>('');
  let copied = $state(false);
  let copyTimer: ReturnType<typeof setTimeout> | null = null;
  let backups = $state<ServiceTuningBackup[]>([]);
  let restoring = $state(false);

  const dirty = $derived(text !== original);
  const latestBackup = $derived(backups[0]);
  const hasBackup = $derived(backups.length > 0 && !loading && !error);
  const canRevert = $derived(dirty && !loading && !error);
  const canReset = $derived(exists && !loading && !error);
  const canSave = $derived(dirty && !loading && !error);

  // Pin the loader's reactive input to the service name string only.
  // Every services WebSocket broadcast passes a fresh svc object even
  // when the name is unchanged; reading svc.name directly inside the
  // effect would re-fire on each push and clobber unsaved edits.
  const currentName = $derived(svc.name);

  // Drop the pending copy-feedback timer on unmount so a 1.5s-late
  // firing doesn't try to mutate $state after the component is gone.
  $effect(() => {
    return () => {
      if (copyTimer) clearTimeout(copyTimer);
    };
  });

  $effect(() => {
    const name = currentName;
    loading = true;
    error = '';
    actionError = '';
    backupsError = '';
    original = '';
    text = '';
    target = '';
    backups = [];
    Promise.all([getServiceConfig(name), loadServiceTuningBackups(name)])
      .then(([cfg, listRes]) => {
        if (currentName !== name) return;
        original = cfg.content;
        text = cfg.content;
        target = cfg.target;
        exists = cfg.exists;
        if (listRes.ok) {
          backups = listRes.list;
        } else {
          backupsError = listRes.error || 'Could not load backups';
        }
      })
      .catch((e) => {
        if (currentName !== name) return;
        error = e instanceof Error ? e.message : 'failed';
      })
      .finally(() => {
        if (currentName === name) loading = false;
      });
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

  async function refreshAfterAction(name: string) {
    try {
      const [cfg, listRes] = await Promise.all([
        getServiceConfig(name),
        loadServiceTuningBackups(name)
      ]);
      if (currentName !== name) return;
      original = cfg.content;
      text = cfg.content;
      target = cfg.target;
      exists = cfg.exists;
      if (listRes.ok) {
        backups = listRes.list;
        backupsError = '';
      } else {
        backupsError = listRes.error || 'Could not load backups';
      }
    } catch (e: unknown) {
      if (currentName !== name) return;
      actionError = e instanceof Error ? e.message : String(e);
    }
  }

  async function restore() {
    // Clear any stale error BEFORE the guard so a click while
    // `backups` is briefly empty (mid-effect, between the reset and
    // the load resolution) doesn't leave an old action error pinned
    // on screen with no way to dismiss it.
    actionError = '';
    if (!latestBackup) return;
    restoring = true;
    try {
      const restoredName = currentName;
      const backupName = latestBackup.name;
      const backupContent = await loadServiceTuningBackupContent(restoredName, backupName);
      openTuningRestoreModal(
        {
          name: restoredName,
          // The diff baseline is `original` (the saved on-disk bytes)
          // not `text` (in-buffer, possibly dirty) so the diff the user
          // previews exactly matches what the backend will write.
          current: original,
          backupName,
          backup: backupContent
        },
        () => refreshAfterAction(restoredName)
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
    const resetName = currentName;
    openTuningResetModal({ name: resetName, path: target }, () =>
      refreshAfterAction(resetName)
    );
  }

  function save() {
    const savedName = currentName;
    openTuningSaveModal(
      { name: savedName, content: text, original, exists },
      () => refreshAfterAction(savedName)
    );
  }
</script>

<div class="flex flex-col h-full">
  <div class="sticky top-0 z-10">
    <div class="flex items-center justify-between bg-gray-50 dark:bg-white/3 px-3 py-1.5 border-b border-gray-200 dark:border-lerd-border">
      <div class="flex items-center gap-2 min-w-0">
        {#if target}
          <span class="text-[10px] text-gray-400 dark:text-gray-600 font-mono truncate" title={target}>{target}</span>
        {/if}
        {#if dirty && !loading && !error}
          <span class="text-[10px] font-medium text-amber-600 dark:text-amber-400">{m.tuningEditor_unsaved()}</span>
        {/if}
        {#if backups.length > 0 && !loading}
          <span
            class="text-[10px] font-medium text-gray-500 dark:text-gray-400"
            title={latestBackup?.name}
          >{m.tuningEditor_backupAvailable({ n: backups.length })}</span>
        {/if}
        {#if backupsError && !loading}
          <span class="text-[10px] font-medium text-red-500 dark:text-red-400" title={backupsError}>{backupsError}</span>
        {/if}
        {#if actionError}
          <span class="text-[10px] font-medium text-red-500 dark:text-red-400" title={actionError}>{actionError}</span>
        {/if}
      </div>
      <div class="flex items-center gap-2 shrink-0">
        <button
          type="button"
          onclick={copy}
          disabled={loading || !!error}
          class="text-xs px-2 py-1 rounded-sm border border-gray-300 dark:border-lerd-border text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-white/5 disabled:opacity-40"
        >
          {copied ? m.common_copied() : m.common_copy()}
        </button>
        {#if canRevert}
          <button
            type="button"
            onclick={revert}
            class="text-xs px-2 py-1 rounded-sm border border-gray-300 dark:border-lerd-border text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-white/5"
          >
            {m.tuningEditor_revert()}
          </button>
        {/if}
        {#if canReset}
          <button
            type="button"
            onclick={reset}
            class="text-xs px-2 py-1 rounded-sm border border-gray-300 dark:border-lerd-border text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-white/5"
          >
            {m.tuningEditor_reset()}
          </button>
        {/if}
        {#if hasBackup}
          <button
            type="button"
            onclick={restore}
            disabled={restoring}
            class="text-xs px-2 py-1 rounded-sm border border-gray-300 dark:border-lerd-border text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-white/5 disabled:opacity-40"
          >
            {m.tuningEditor_restore()}
          </button>
        {/if}
        {#if canSave}
          <button
            type="button"
            onclick={save}
            class="text-xs px-3 py-1 rounded-sm bg-lerd-red hover:bg-lerd-redhov text-white transition-colors"
          >
            {m.common_save()}
          </button>
        {/if}
      </div>
    </div>
  </div>

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
