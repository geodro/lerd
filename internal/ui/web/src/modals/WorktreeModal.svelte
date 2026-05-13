<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import { closeModal } from '$stores/modals';
  import { sites, loadSites, type Site } from '$stores/sites';
  import {
    worktreeOptions,
    removeWorktree,
    streamWorktreeAdd,
    type WorktreeOptions
  } from '$stores/worktree';
  import { m } from '../paraglide/messages.js';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  // Re-read the live site from the store so the list reflects add/remove.
  const cur = $derived($sites.find((s) => s.domain === site.domain) ?? site);
  const worktrees = $derived(cur.worktrees ?? []);

  type View = 'list' | 'add';
  let view = $state<View>('list');
  let error = $state('');

  // ---- remove ----
  let removingBranch = $state('');
  let confirmBranch = $state(''); // branch whose inline confirm is open
  let confirmForce = $state(false);
  let confirmDropDB = $state(false);

  function openConfirm(branch: string) {
    confirmBranch = branch;
    confirmForce = false;
    confirmDropDB = false;
  }

  async function doRemove(branch: string) {
    removingBranch = branch;
    error = '';
    try {
      const res = await removeWorktree(cur.domain, branch, {
        force: confirmForce,
        dropDB: confirmDropDB
      });
      if (!res.ok) {
        error = res.error || m.common_failed();
        return;
      }
      confirmBranch = '';
      await loadSites();
    } finally {
      removingBranch = '';
    }
  }

  // ---- add ----
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
  let warnings = $state<string[]>([]);
  let logs = $state<string[]>([]);
  let scrollEl: HTMLDivElement | null = $state(null);
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
      // Keep the selected db value if still offered, else fall back.
      const dbo = o.db_options ?? [];
      if (!dbo.some((d) => d.value === db)) db = dbo[0]?.value ?? 'share';
    } catch (e) {
      error = e instanceof Error ? e.message : m.common_failed();
    } finally {
      optsLoading = false;
    }
  }

  function startAdd() {
    view = 'add';
    error = '';
    logs = [];
    warnings = [];
    finished = false;
    branchMode = 'new';
    newBranch = '';
    db = 'share';
    migrate = false;
    void loadOpts();
  }

  function backToList() {
    view = 'list';
    finished = false;
    error = '';
    warnings = [];
    logs = [];
  }

  // When the new-branch name changes, refetch db options for that branch
  // (so a preserved isolated DB surfaces reuse/reset, like the CLI).
  function onNewBranchInput() {
    clearTimeout(optsTimer);
    optsTimer = setTimeout(() => void loadOpts(newBranch.trim()), 400);
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
            box.result = {
              ok: ev.ok,
              branch: ev.branch,
              error: ev.error,
              warnings: ev.warnings
            };
            return;
          }
          if (ev.line !== undefined) {
            logs = [...logs, ev.line];
            requestAnimationFrame(() => {
              if (scrollEl) scrollEl.scrollTop = scrollEl.scrollHeight;
            });
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
      // Stay on the logs view when anything emitted a [WARN] so the user
      // can read the messages instead of the modal silently jumping back
      // to the list with a half-finished setup.
      if (warnings.length > 0) {
        finished = true;
        return;
      }
      view = 'list';
    } catch (e) {
      error = e instanceof Error ? e.message : m.common_failed();
      finished = true;
    } finally {
      creating = false;
    }
  }

  function currentBranchLabel(b: string): string {
    return opts && b === opts.default_branch_label ? m.worktreeMgr_currentBranch({ branch: b }) : b;
  }

  // Worktree subdomains inherit the parent site's TLS state, so honour the
  // secure toggle when building the URL.
  function worktreeUrl(domain: string): string {
    return (cur.tls ? 'https://' : 'http://') + domain;
  }
</script>

<Modal open title={m.worktreeMgr_title()} onclose={closeModal} size="lg">
  {#if view === 'list'}
    <div class="px-5 py-3 max-h-[60vh] overflow-y-auto">
      {#if worktrees.length === 0}
        <p class="py-6 text-center text-sm text-gray-400">{m.worktreeMgr_listEmpty()}</p>
      {:else}
        <div class="divide-y divide-gray-100 dark:divide-lerd-border">
          {#each worktrees as wt (wt.branch)}
            <div class="py-2.5">
              <div class="flex items-center gap-2">
                <svg class="w-3.5 h-3.5 shrink-0 text-violet-400" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">
                  <path d="M6 3v12M15 6a3 3 0 1 0 6 0a3 3 0 1 0-6 0M3 18a3 3 0 1 0 6 0a3 3 0 1 0-6 0M18 9a9 9 0 0 1-9 9"/>
                </svg>
                <span class="font-mono text-sm text-gray-800 dark:text-gray-200 truncate">{wt.branch}</span>
                {#if wt.db_isolated}
                  <span class="shrink-0 text-[10px] uppercase tracking-wider rounded-sm px-1.5 py-0.5 bg-violet-100 dark:bg-violet-500/15 text-violet-600 dark:text-violet-300">{m.worktreeMgr_isolatedDbBadge()}</span>
                {/if}
                <a
                  href={worktreeUrl(wt.domain ?? '')}
                  onclick={(e) => {
                    e.preventDefault();
                    window.open(worktreeUrl(wt.domain ?? ''), '_blank', 'noopener');
                  }}
                  class="ml-auto pl-2 font-mono text-[11px] text-gray-400 hover:text-lerd-red truncate transition-colors"
                  title={wt.domain}
                >{wt.domain}</a>
                {#if confirmBranch !== wt.branch}
                  <button
                    onclick={() => openConfirm(wt.branch ?? '')}
                    disabled={removingBranch === wt.branch}
                    class="shrink-0 text-gray-400 hover:text-red-500 transition-colors disabled:opacity-50"
                    title={m.common_remove()}
                    aria-label={m.common_remove()}
                  >
                    <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/>
                    </svg>
                  </button>
                {/if}
              </div>
              {#if confirmBranch === wt.branch}
                <div class="mt-2 ml-5 rounded-lg border border-gray-200 dark:border-lerd-border bg-gray-50 dark:bg-white/5 px-3 py-2.5 space-y-2">
                  <p class="text-xs text-gray-600 dark:text-gray-300">{m.worktreeMgr_removeTitle({ branch: wt.branch ?? '' })}</p>
                  <label class="flex items-center gap-2 text-xs text-gray-600 dark:text-gray-400">
                    <input type="checkbox" bind:checked={confirmForce} class="rounded-sm border-gray-300 dark:border-lerd-border" />
                    {m.worktreeMgr_force()}
                  </label>
                  {#if wt.db_isolated}
                    <label class="flex items-center gap-2 text-xs text-gray-600 dark:text-gray-400">
                      <input type="checkbox" bind:checked={confirmDropDB} class="rounded-sm border-gray-300 dark:border-lerd-border" />
                      {m.worktreeMgr_dropDb({ db: wt.db_database ?? '' })}
                    </label>
                  {/if}
                  <div class="flex items-center gap-2 pt-0.5">
                    <DetailButton onclick={() => (confirmBranch = '')} disabled={removingBranch === wt.branch}>{m.common_cancel()}</DetailButton>
                    <DetailButton tone="danger" onclick={() => doRemove(wt.branch ?? '')} loading={removingBranch === wt.branch} disabled={removingBranch === wt.branch}>
                      {removingBranch === wt.branch ? m.worktreeMgr_removing() : m.common_remove()}
                    </DetailButton>
                  </div>
                </div>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {:else if !creating && !finished}
    <div class="px-5 py-4 space-y-4 max-h-[60vh] overflow-y-auto">
      <!-- branch -->
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
            <select bind:value={baseRef} class="w-full text-sm bg-white dark:bg-lerd-bg border border-gray-200 dark:border-lerd-border rounded-sm px-2 py-1.5 text-gray-700 dark:text-gray-300">
              <option value="">{currentBranchLabel(opts?.default_branch_label || 'HEAD')}</option>
              {#each localBranches as b (b)}
                <option value={b}>{b}</option>
              {/each}
              {#if remoteBranches.length}
                <optgroup label="remote">
                  {#each remoteBranches as b (b)}
                    <option value={b}>{b}</option>
                  {/each}
                </optgroup>
              {/if}
            </select>
          {/if}
        {:else}
          <select bind:value={existingBranch} class="w-full text-sm bg-white dark:bg-lerd-bg border border-gray-200 dark:border-lerd-border rounded-sm px-2 py-1.5 text-gray-700 dark:text-gray-300">
            {#each localBranches as b (b)}
              <option value={b}>{b}</option>
            {/each}
            {#if remoteBranches.length}
              <optgroup label="remote">
                {#each remoteBranches as b (b)}
                  <option value={b}>{b}</option>
                {/each}
              </optgroup>
            {/if}
          </select>
        {/if}
      </div>

      <!-- database -->
      <div class="space-y-1.5">
        <div class="text-xs font-medium text-gray-500 dark:text-gray-400">{m.worktreeMgr_databaseHeading()}</div>
        <select bind:value={db} class="w-full text-sm bg-white dark:bg-lerd-bg border border-gray-200 dark:border-lerd-border rounded-sm px-2 py-1.5 text-gray-700 dark:text-gray-300">
          {#if dbOptions.length}
            {#each dbOptions as o (o.value)}
              <option value={o.value}>{o.label}</option>
            {/each}
          {:else}
            <option value="share">…</option>
          {/if}
        </select>
        {#if showMigrate}
          <label class="flex items-center gap-2 text-xs text-gray-600 dark:text-gray-400 pt-0.5">
            <input type="checkbox" bind:checked={migrate} class="rounded-sm border-gray-300 dark:border-lerd-border" />
            {m.worktreeMgr_runMigrations()}
          </label>
        {/if}
      </div>

      <!-- assets -->
      <div class="space-y-1.5">
        <div class="text-xs font-medium text-gray-500 dark:text-gray-400">{m.worktreeMgr_assetsHeading()}</div>
        <select bind:value={build} class="w-full text-sm bg-white dark:bg-lerd-bg border border-gray-200 dark:border-lerd-border rounded-sm px-2 py-1.5 text-gray-700 dark:text-gray-300">
          {#if buildOptions.length}
            {#each buildOptions as o (o.value)}
              <option value={o.value}>{o.label}</option>
            {/each}
          {:else}
            <option value="auto">…</option>
          {/if}
        </select>
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
      <div
        bind:this={scrollEl}
        class="bg-gray-50 dark:bg-black/30 rounded-lg p-3 max-h-72 overflow-y-auto font-mono text-xs text-gray-600 dark:text-gray-400 space-y-0.5"
      >
        {#each logs as line, i (i)}
          <div>{line}</div>
        {/each}
        {#if logs.length === 0}
          <div class="text-gray-400 dark:text-gray-500">{m.link_waitingOutput()}</div>
        {/if}
      </div>
    </div>
  {/if}

  {#if error && !finished}
    <div class="px-5 pb-1"><p class="text-xs text-red-500">{error}</p></div>
  {/if}

  {#snippet footer()}
    {#if view === 'list'}
      <DetailButton onclick={closeModal}>{m.common_close()}</DetailButton>
      <DetailButton tone="primary" onclick={startAdd} disabled={confirmBranch !== ''}>{m.worktreeMgr_add()}</DetailButton>
    {:else if finished}
      <DetailButton tone="primary" onclick={backToList}>{m.worktreeMgr_backToList()}</DetailButton>
    {:else}
      {#if !creating}
        <DetailButton onclick={() => (view = 'list')}>{m.common_cancel()}</DetailButton>
      {/if}
      <DetailButton tone="primary" onclick={create} disabled={!canCreate || optsLoading} loading={creating}>
        {creating ? m.worktreeMgr_creating() : m.worktreeMgr_create()}
      </DetailButton>
    {/if}
  {/snippet}
</Modal>
