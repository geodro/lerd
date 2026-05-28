<script lang="ts">
  import EnvEditor from '$components/EnvEditor.svelte';
  import Dropdown from '$components/Dropdown.svelte';
  import {
    loadSiteEnv,
    loadSiteEnvFiles,
    loadSiteEnvBackups,
    loadSiteEnvBackupContent,
    type Site,
    type SiteEnvBackup
  } from '$stores/sites';
  import { openEnvSaveModal, openEnvRestoreModal } from '$stores/modals';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
    branch: string;
  }
  let { site, branch }: Props = $props();

  let files = $state<string[]>(['.env']);
  let file = $state<string>('.env');
  let original = $state<string>('');
  let text = $state<string>('');
  let loading = $state(true);
  let error = $state<string>('');
  let copied = $state(false);
  let copyTimer: ReturnType<typeof setTimeout> | null = null;
  let backups = $state<SiteEnvBackup[]>([]);
  let restoring = $state(false);

  const envPath = $derived.by(() => {
    if (branch) {
      const wt = (site.worktrees || []).find((w) => w.branch === branch);
      if (wt?.path) return wt.path + '/' + file;
    }
    return (site.path || '') + '/' + file;
  });

  const dirty = $derived(text !== original);
  const latestBackup = $derived(backups[0]);
  const canRevert = $derived((dirty || backups.length > 0) && !loading && !error);

  // Refresh the file list whenever the site or branch changes. When the
  // selected file disappears we only snap back to .env if there are no
  // unsaved edits; a dirty buffer for a file that vanished on disk stays
  // open so the user can copy out or save to recreate.
  $effect(() => {
    const domain = site.domain;
    const b = branch;
    loadSiteEnvFiles(domain, b).then((list) => {
      if (site.domain !== domain || branch !== b) return;
      files = list;
      if (!list.includes(file) && !dirty) file = '.env';
    });
  });

  // Reload content + backups whenever the chosen file (or site/branch) changes.
  $effect(() => {
    const domain = site.domain;
    const b = branch;
    const f = file;
    loading = true;
    error = '';
    original = '';
    text = '';
    backups = [];
    Promise.all([loadSiteEnv(domain, b, f), loadSiteEnvBackups(domain, b, f)])
      .then(([t, list]) => {
        if (site.domain !== domain || branch !== b || file !== f) return;
        original = t;
        text = t;
        backups = list;
      })
      .catch((e: unknown) => {
        // Guard the error setter the same way the success branch does, so
        // a stale rejection from a previous site cannot blow away the
        // current view's error state.
        if (site.domain !== domain || branch !== b || file !== f) return;
        error = e instanceof Error ? e.message : String(e);
      })
      .finally(() => {
        if (site.domain === domain && branch === b && file === f) loading = false;
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

  async function revert() {
    if (latestBackup) {
      // The diff modal's "current" baseline is the on-screen text (which
      // includes any unsaved edits), so accepting the restore visibly
      // discards everything the user could still see — no silent loss.
      // The backup content is loaded into the modal so the modal does not
      // need its own loader.
      restoring = true;
      try {
        // Snapshot the file the user is on now; if it changes during the
        // network round-trip the success callback should still apply to
        // the file we restored, not whatever is current at completion.
        const restoredFile = file;
        const restoredBranch = branch;
        const restoredDomain = site.domain;
        const backupContent = await loadSiteEnvBackupContent(
          restoredDomain,
          latestBackup.name,
          restoredBranch,
          restoredFile
        );
        openEnvRestoreModal(
          {
            domain: restoredDomain,
            branch: restoredBranch,
            file: restoredFile,
            current: text,
            backupName: latestBackup.name,
            backup: backupContent
          },
          async () => {
            // Only refresh local state if the user is still looking at
            // the file we restored; if they navigated away, the next
            // load effect for the new context will populate fresh state.
            if (
              site.domain !== restoredDomain ||
              branch !== restoredBranch ||
              file !== restoredFile
            ) {
              return;
            }
            // The restore endpoint already returns the new content, so
            // we use the backupContent we loaded for the diff instead of
            // re-fetching via loadSiteEnv. We do refetch the backups list
            // because it shrank by one.
            original = backupContent;
            text = backupContent;
            backups = await loadSiteEnvBackups(restoredDomain, restoredBranch, restoredFile);
          }
        );
      } catch (e: unknown) {
        error = e instanceof Error ? e.message : String(e);
      } finally {
        restoring = false;
      }
      return;
    }
    text = original;
  }

  function save() {
    // Snapshot the file we are saving so a concurrent file-list refresh
    // (or any other reactive change) cannot redirect the post-save reload
    // at the wrong file.
    const savedDomain = site.domain;
    const savedBranch = branch;
    const savedFile = file;
    openEnvSaveModal(
      { domain: savedDomain, branch: savedBranch, file: savedFile, content: text, original },
      async () => {
        const [t, list] = await Promise.all([
          loadSiteEnv(savedDomain, savedBranch, savedFile),
          loadSiteEnvBackups(savedDomain, savedBranch, savedFile)
        ]);
        // Only apply if the user is still on the file we saved; otherwise
        // the load effect for the new file will populate its own state.
        if (
          site.domain !== savedDomain ||
          branch !== savedBranch ||
          file !== savedFile
        ) {
          return;
        }
        original = t;
        text = t;
        backups = list;
        // A save does not change the set of env files (we only edit
        // existing ones from the dropdown), so no need to refetch /files.
      }
    );
  }
</script>

<div class="flex flex-col h-full">
  <div class="sticky top-0 z-10">
    <div class="flex items-center justify-between bg-gray-50 dark:bg-white/3 px-3 py-1.5 border-b border-gray-200 dark:border-lerd-border">
      <div class="flex items-center gap-2">
        <Dropdown
          value={file}
          options={files}
          disabled={loading || files.length <= 1}
          onchange={(v) => (file = v)}
        />
        {#if dirty && !loading && !error}
          <span class="text-[10px] font-medium text-amber-600 dark:text-amber-400">{m.envEditor_unsaved()}</span>
        {/if}
        {#if backups.length > 0 && !loading}
          <span
            class="text-[10px] font-medium text-gray-500 dark:text-gray-400"
            title={latestBackup?.name}
          >{m.envEditor_backupAvailable({ n: backups.length })}</span>
        {/if}
      </div>
      <div class="flex items-center gap-2">
        <button
          type="button"
          onclick={copy}
          disabled={loading || !!error}
          class="text-xs px-2 py-1 rounded-sm border border-gray-300 dark:border-lerd-border text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-white/5 disabled:opacity-40"
        >
          {copied ? m.common_copied() : m.common_copy()}
        </button>
        <button
          type="button"
          onclick={revert}
          disabled={!canRevert || restoring}
          class="text-xs px-2 py-1 rounded-sm border border-gray-300 dark:border-lerd-border text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-white/5 disabled:opacity-40"
        >
          {m.envEditor_revert()}
        </button>
        <button
          type="button"
          onclick={save}
          disabled={!dirty || loading}
          class="text-xs px-3 py-1 rounded-sm bg-lerd-red hover:bg-lerd-redhov text-white disabled:opacity-40 transition-colors"
        >
          {m.common_save()}
        </button>
      </div>
    </div>
  </div>

  <div class="flex-1 overflow-hidden bg-gray-50 dark:bg-black/40">
    {#if loading}
      <p class="text-xs text-gray-400 px-3 py-2.5">{m.common_loading()}</p>
    {:else if error}
      <p class="text-xs text-red-500 dark:text-red-400 px-3 py-2.5">{error}</p>
    {:else}
      {#if !original}
        <p class="text-xs text-gray-400 px-3 py-2.5">
          {m.sites_env_missing({ path: envPath })}
        </p>
      {/if}
      <div class="h-full min-h-64">
        <EnvEditor bind:value={text} />
      </div>
    {/if}
  </div>
</div>
