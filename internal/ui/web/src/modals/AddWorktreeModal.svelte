<script lang="ts">
  import { onMount } from 'svelte';
  import Modal from '$components/Modal.svelte';
  import Dropdown from '$components/Dropdown.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import BuildLog from '$components/BuildLog.svelte';
  import { closeModal } from '$stores/modals';
  import { sites, loadSites, type Site } from '$stores/sites';
  import {
    worktreeOptions,
    streamWorktreeAdd,
    type WorktreeOptions
  } from '$stores/worktree';
  import { m } from '../paraglide/messages.js';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  const cur = $derived($sites.find((s) => s.domain === site.domain) ?? site);

  let opts = $state<WorktreeOptions | null>(null);
  let optsLoading = $state(false);
  let branchMode = $state<'new' | 'existing'>('new');
  let newBranch = $state('');
  let baseRef = $state('');
  let existingBranch = $state('');
  let db = $state('share');
  let migrate = $state(false);
  let build = $state('auto');
  let creating = $state(false);
  let finished = $state(false);
  let error = $state('');
  let warnings = $state<string[]>([]);
  let logs = $state<string[]>([]);
  let optsTimer: ReturnType<typeof setTimeout> | undefined;

  const localBranches = $derived(opts?.local_branches ?? []);
  const remoteBranches = $derived(opts?.remote_branches ?? []);
  const hasAnyBranch = $derived(localBranches.length + remoteBranches.length > 0);
  const buildOptions = $derived(opts?.build_options ?? []);
  const dbOptions = $derived(opts?.db_options ?? []);
  const showMigrate = $derived(Boolean(opts?.can_migrate) && (db === 'empty' || db === 'reset'));
  const canCreate = $derived(
    !creating &&
      ((branchMode === 'new' && newBranch.trim() !== '') ||
        (branchMode === 'existing' && existingBranch !== ''))
  );

  async function loadOpts(branch = '') {
    optsLoading = true;
    try {
      const o = await worktreeOptions(cur.domain, branch);
      opts = o;
      if (!branch) {
        build = o.build_default || 'auto';
        baseRef = '';
        existingBranch = (o.local_branches ?? [])[0] ?? (o.remote_branches ?? [])[0] ?? '';
      }
      const dbo = o.db_options ?? [];
      if (!dbo.some((d) => d.value === db)) db = dbo[0]?.value ?? 'share';
    } catch (e) {
      error = e instanceof Error ? e.message : m.common_failed();
    } finally {
      optsLoading = false;
    }
  }

  function onNewBranchInput() {
    clearTimeout(optsTimer);
    optsTimer = setTimeout(() => void loadOpts(newBranch.trim()), 400);
  }

  function currentBranchLabel(b: string): string {
    return opts && b === opts.default_branch_label ? m.worktreeMgr_currentBranch({ branch: b }) : b;
  }

  async function create() {
    creating = true;
    finished = false;
    error = '';
    warnings = [];
    logs = [];
    try {
      const box: {
        result: { ok?: boolean; branch?: string; error?: string; warnings?: string[] } | null;
      } = { result: null };
      await streamWorktreeAdd(
        cur.domain,
        {
          newBranch: branchMode === 'new' ? newBranch.trim() : undefined,
          existingBranch: branchMode === 'existing' ? existingBranch : undefined,
          baseRef: branchMode === 'new' && baseRef ? baseRef : undefined,
          db,
          migrate: showMigrate ? migrate : false,
          build
        },
        (ev) => {
          if (ev.done) {
            box.result = { ok: ev.ok, branch: ev.branch, error: ev.error, warnings: ev.warnings };
            return;
          }
          if (ev.line !== undefined) {
            logs = [...logs, ev.line];
          }
        }
      );
      const result = box.result;
      await loadSites();
      warnings = result?.warnings ?? [];
      if (!result || !result.ok) {
        error = result?.error || m.worktreeMgr_createFailed();
        finished = true;
        return;
      }
      if (warnings.length > 0) {
        finished = true;
        return;
      }
      closeModal();
    } catch (e) {
      error = e instanceof Error ? e.message : m.common_failed();
      finished = true;
    } finally {
      creating = false;
    }
  }

  onMount(() => {
    void loadOpts();
  });
</script>

<Modal open title={m.worktreeMgr_add()} onclose={closeModal} size="lg">
  {#if !creating && !finished}
    <div class="px-5 py-4 space-y-4 max-h-[60vh] overflow-y-auto">
      <div class="space-y-1.5">
        <div class="text-xs font-medium text-gray-500 dark:text-gray-400">{m.worktreeMgr_branchHeading()}</div>
        <div class="flex gap-3 text-sm">
          <label class="flex items-center gap-1.5 text-gray-700 dark:text-gray-300">
            <input type="radio" value="new" bind:group={branchMode} /> {m.worktreeMgr_newBranchOpt()}
          </label>
          <label class="flex items-center gap-1.5 {hasAnyBranch ? 'text-gray-700 dark:text-gray-300' : 'text-gray-400 dark:text-gray-600'}">
            <input type="radio" value="existing" bind:group={branchMode} disabled={!hasAnyBranch} /> {m.worktreeMgr_existingBranchOpt()}
          </label>
        </div>
        {#if branchMode === 'new'}
          <input
            type="text"
            bind:value={newBranch}
            oninput={onNewBranchInput}
            placeholder={m.worktreeMgr_branchNamePlaceholder()}
            class="w-full text-sm bg-white dark:bg-lerd-bg border border-gray-200 dark:border-lerd-border rounded-sm px-2 py-1.5 text-gray-700 dark:text-gray-300 focus:outline-hidden focus:border-lerd-red/50"
          />
          {#if hasAnyBranch}
            <div class="text-xs text-gray-400 pt-1">{m.worktreeMgr_basedOn()}</div>
            <Dropdown
              value={baseRef}
              width="full"
              options={[
                { value: '', label: currentBranchLabel(opts?.default_branch_label || 'HEAD') },
                ...localBranches.map((b) => ({ value: b, label: b })),
                ...remoteBranches.map((b) => ({ value: b, label: b, description: 'remote' }))
              ]}
              onchange={(v) => (baseRef = v)}
            />
          {/if}
        {:else}
          <Dropdown
            value={existingBranch}
            width="full"
            options={[
              ...localBranches.map((b) => ({ value: b, label: b })),
              ...remoteBranches.map((b) => ({ value: b, label: b, description: 'remote' }))
            ]}
            onchange={(v) => (existingBranch = v)}
          />
        {/if}
      </div>

      <div class="space-y-1.5">
        <div class="text-xs font-medium text-gray-500 dark:text-gray-400">{m.worktreeMgr_databaseHeading()}</div>
        <Dropdown
          value={db}
          width="full"
          options={dbOptions.length ? dbOptions : [{ value: 'share', label: '…' }]}
          onchange={(v) => (db = v)}
        />
        {#if showMigrate}
          <label class="flex items-center gap-2 text-xs text-gray-600 dark:text-gray-400 pt-0.5">
            <input type="checkbox" bind:checked={migrate} class="rounded-sm border-gray-300 dark:border-lerd-border" />
            {m.worktreeMgr_runMigrations()}
          </label>
        {/if}
      </div>

      <div class="space-y-1.5">
        <div class="text-xs font-medium text-gray-500 dark:text-gray-400">{m.worktreeMgr_assetsHeading()}</div>
        <Dropdown
          value={build}
          width="full"
          options={buildOptions.length ? buildOptions : [{ value: 'auto', label: '…' }]}
          onchange={(v) => (build = v)}
        />
      </div>
    </div>
  {:else}
    <div class="px-5 py-3 space-y-2">
      {#if finished && error}
        <div class="rounded-lg border border-red-200 dark:border-red-500/30 bg-red-50 dark:bg-red-500/10 px-3 py-2 text-xs text-red-700 dark:text-red-300">
          {error}
        </div>
      {:else if finished && warnings.length > 0}
        <div class="rounded-lg border border-amber-200 dark:border-amber-500/30 bg-amber-50 dark:bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-300">
          {m.worktreeMgr_completedWithWarnings()}
        </div>
      {/if}
      <BuildLog {logs} />
    </div>
  {/if}

  {#if error && !finished}
    <div class="px-5 pb-1"><p class="text-xs text-red-500">{error}</p></div>
  {/if}

  {#snippet footer()}
    {#if finished}
      <DetailButton tone="primary" onclick={closeModal}>{m.common_close()}</DetailButton>
    {:else if !creating}
      <DetailButton onclick={closeModal}>{m.common_cancel()}</DetailButton>
      <DetailButton tone="primary" onclick={create} disabled={!canCreate || optsLoading} loading={creating}>
        {m.worktreeMgr_create()}
      </DetailButton>
    {:else}
      <DetailButton tone="primary" disabled loading={true}>{m.worktreeMgr_creating()}</DetailButton>
    {/if}
  {/snippet}
</Modal>
