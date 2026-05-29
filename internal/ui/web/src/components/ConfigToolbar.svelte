<script lang="ts">
  import { m } from '../paraglide/messages.js';

  interface Props {
    path: string;
    dirty: boolean;
    loading: boolean;
    error: string;
    backupCount: number;
    latestBackupName?: string;
    backupsError: string;
    actionError: string;
    canRevert: boolean;
    canReset: boolean;
    canSave: boolean;
    hasBackup: boolean;
    restoring: boolean;
    copied: boolean;
    onCopy: () => void;
    onRevert: () => void;
    onReset: () => void;
    onRestore: () => void;
    onSave: () => void;
  }
  let {
    path,
    dirty,
    loading,
    error,
    backupCount,
    latestBackupName = '',
    backupsError,
    actionError,
    canRevert,
    canReset,
    canSave,
    hasBackup,
    restoring,
    copied,
    onCopy,
    onRevert,
    onReset,
    onRestore,
    onSave
  }: Props = $props();
</script>

<div class="sticky top-0 z-10">
  <div class="flex items-center justify-between bg-gray-50 dark:bg-white/3 px-3 py-1.5 border-b border-gray-200 dark:border-lerd-border">
    <div class="flex items-center gap-2 min-w-0">
      {#if path}
        <span class="text-[10px] text-gray-400 dark:text-gray-600 font-mono truncate" title={path}>{path}</span>
      {/if}
      {#if dirty && !loading && !error}
        <span class="text-[10px] font-medium text-amber-600 dark:text-amber-400">{m.nginxEditor_unsaved()}</span>
      {/if}
      {#if backupCount > 0 && !loading}
        <span class="text-[10px] font-medium text-gray-500 dark:text-gray-400" title={latestBackupName}
          >{m.nginxEditor_backupAvailable({ n: backupCount })}</span
        >
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
        onclick={onCopy}
        disabled={loading || !!error}
        class="text-xs px-2 py-1 rounded-sm border border-gray-300 dark:border-lerd-border text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-white/5 disabled:opacity-40"
      >
        {copied ? m.common_copied() : m.common_copy()}
      </button>
      {#if canRevert}
        <button
          type="button"
          onclick={onRevert}
          class="text-xs px-2 py-1 rounded-sm border border-gray-300 dark:border-lerd-border text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-white/5"
        >
          {m.nginxEditor_revert()}
        </button>
      {/if}
      {#if canReset}
        <button
          type="button"
          onclick={onReset}
          class="text-xs px-2 py-1 rounded-sm border border-gray-300 dark:border-lerd-border text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-white/5"
        >
          {m.nginxEditor_reset()}
        </button>
      {/if}
      {#if hasBackup}
        <button
          type="button"
          onclick={onRestore}
          disabled={restoring}
          class="text-xs px-2 py-1 rounded-sm border border-gray-300 dark:border-lerd-border text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-white/5 disabled:opacity-40"
        >
          {m.nginxEditor_restore()}
        </button>
      {/if}
      {#if canSave}
        <button
          type="button"
          onclick={onSave}
          class="text-xs px-3 py-1 rounded-sm bg-lerd-red hover:bg-lerd-redhov text-white transition-colors"
        >
          {m.common_save()}
        </button>
      {/if}
    </div>
  </div>
</div>
