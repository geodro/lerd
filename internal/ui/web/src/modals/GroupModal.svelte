<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import Dropdown from '$components/Dropdown.svelte';
  import Icon from '$components/Icon.svelte';
  import Toggle from '$components/Toggle.svelte';
  import { closeModal } from '$stores/modals';
  import { loadSites, sites, isGroupSecondary, type Site } from '$stores/sites';
  import {
    assignGroup,
    unassignGroup,
    setGroupLabel,
    setGroupSharedDB,
    dissolveGroup
  } from '$stores/grouping';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  // Track the latest record so we survive primary-domain renames across reloads.
  const current = $derived(
    $sites.find((s) => site.name && s.name === site.name) ??
      $sites.find((s) => s.domain === site.domain) ??
      site
  );

  const asSecondary = $derived(isGroupSecondary(current));
  const secondaries = $derived(
    $sites.filter((s) => current.group && s.group === current.group && isGroupSecondary(s))
  );
  const candidates = $derived(
    $sites.filter((s) => !s.group && s.name !== current.name).map((s) => ({
      value: s.domain,
      label: s.name || s.domain
    }))
  );
  // Sites this one could be grouped under as a secondary: anything that isn't
  // itself a secondary and isn't this site.
  const mainCandidates = $derived(
    $sites.filter((s) => s.name !== current.name && !s.group_subdomain).map((s) => ({
      value: s.domain,
      label: s.name || s.domain
    }))
  );
  const ungrouped = $derived(!current.group);
  const pickMainSite = $derived($sites.find((x) => x.domain === pickMain));

  // For an ungrouped site the modal can either make it the group main (add
  // secondaries under it) or make it a secondary of another site.
  let mode = $state<'main' | 'secondary'>('main');

  let pickDomain = $state('');
  let pickMain = $state('');
  let pickLabel = $state('');
  let pickShareDB = $state(false);
  let editDomain = $state('');
  let editValue = $state('');
  let selfLabel = $state('');

  // Seed the secondary's own label field once from the current record.
  let seeded = false;
  $effect(() => {
    if (asSecondary && !seeded) {
      selfLabel = current.group_subdomain || '';
      seeded = true;
    }
  });
  let loading = $state(false);
  let error = $state('');
  let flash = $state('');
  let flashTimer: ReturnType<typeof setTimeout> | null = null;

  function showFlash(msg: string) {
    flash = msg;
    if (flashTimer) clearTimeout(flashTimer);
    flashTimer = setTimeout(() => (flash = ''), 3000);
  }

  // Mirror the backend git.SanitizeBranch so a label the UI accepts is one the
  // server's ValidateLabel will too (lowercase, [a-z0-9-], collapsed, 50 max).
  function sanitizeLabel(s: string): string {
    return s
      .toLowerCase()
      .replace(/[^a-z0-9-]/g, '-')
      .replace(/-+/g, '-')
      .replace(/^-|-$/g, '')
      .slice(0, 50)
      .replace(/-+$/, '');
  }

  // Pre-fill the label from the secondary's name, stripping the main's name
  // when it appears as a prefix/suffix (admin-astrolov -> admin under astrolov).
  function deriveLabel(secondaryName: string, mainName: string): string {
    let n = (secondaryName || '').toLowerCase();
    const mn = (mainName || '').toLowerCase();
    if (mn && n.endsWith('-' + mn)) n = n.slice(0, -(mn.length + 1));
    else if (mn && n.startsWith(mn + '-')) n = n.slice(mn.length + 1);
    else if (n.includes('-')) n = n.split('-')[0];
    return sanitizeLabel(n) || 'app';
  }

  function onPick(v: string) {
    pickDomain = v;
    const s = $sites.find((x) => x.domain === v);
    pickLabel = deriveLabel(s?.name || '', current.name || '');
  }

  function onPickMain(v: string) {
    pickMain = v;
    const m = $sites.find((x) => x.domain === v);
    pickLabel = deriveLabel(current.name || '', m?.name || '');
  }

  async function runAction(fn: () => Promise<{ ok: boolean; error?: string }>, successMsg: string) {
    loading = true;
    error = '';
    try {
      const r = await fn();
      if (!r.ok) {
        error = r.error || 'Failed';
        return;
      }
      await loadSites();
      showFlash(successMsg);
    } finally {
      loading = false;
    }
  }

  async function addSecondary() {
    const label = sanitizeLabel(pickLabel);
    if (!pickDomain || !label) return;
    await runAction(() => assignGroup(current, pickDomain, label, pickShareDB), 'Added to group');
    if (!error) {
      pickDomain = '';
      pickLabel = '';
      pickShareDB = false;
    }
  }
  async function groupUnderMain() {
    const label = sanitizeLabel(pickLabel);
    const mainSite = $sites.find((x) => x.domain === pickMain);
    if (!mainSite || !label) return;
    await runAction(() => assignGroup(mainSite, current.domain, label, pickShareDB), 'Grouped');
    if (!error) {
      pickMain = '';
      pickLabel = '';
      pickShareDB = false;
    }
  }
  async function toggleSharedDB(sec: Site) {
    await runAction(() => setGroupSharedDB(sec, !sec.group_shared_db), 'Database updated');
  }

  function startEdit(sec: Site) {
    editDomain = sec.domain;
    editValue = sec.group_subdomain || '';
  }
  function cancelEdit() {
    editDomain = '';
    editValue = '';
  }
  async function saveEdit(sec: Site) {
    const label = sanitizeLabel(editValue);
    if (!label || label === sec.group_subdomain) {
      cancelEdit();
      return;
    }
    await runAction(() => setGroupLabel(sec, label), 'Subdomain updated');
    if (!error) cancelEdit();
  }
  async function saveSelfLabel() {
    const label = sanitizeLabel(selfLabel);
    if (!label || label === current.group_subdomain) return;
    await runAction(() => setGroupLabel(current, label), 'Subdomain updated');
  }
  async function remove(sec: Site) {
    await runAction(() => unassignGroup(sec), 'Removed from group');
  }
  async function unassignSelf() {
    await runAction(() => unassignGroup(current), 'Removed from group');
    if (!error) closeModal();
  }
  async function dissolve() {
    await runAction(() => dissolveGroup(current), 'Group dissolved');
  }
</script>

<Modal open title="Group" onclose={closeModal}>
  {#if asSecondary}
    <div class="px-5 py-4 space-y-3">
      <p class="text-sm text-gray-600 dark:text-gray-300">
        This site is a secondary of
        <span class="font-mono font-semibold text-gray-800 dark:text-gray-100">{current.group_main_domain}</span>.
      </p>
      <div class="flex items-center gap-2">
        <input
          bind:value={selfLabel}
          onkeydown={(e) => e.key === 'Enter' && saveSelfLabel()}
          placeholder={current.group_subdomain}
          disabled={loading}
          class="flex-1 text-sm font-mono bg-transparent border border-gray-200 dark:border-lerd-border rounded-sm px-2 py-1.5 text-gray-700 dark:text-gray-300 focus:outline-hidden focus:border-lerd-red/50"
        />
        <span class="text-sm text-gray-400 shrink-0">.{current.group_main_domain}</span>
        <DetailButton
          tone="primary"
          onclick={saveSelfLabel}
          disabled={loading || !selfLabel.trim()}>Save</DetailButton>
      </div>
      <div class="flex items-center justify-between gap-2 pt-1">
        <span class="text-sm text-gray-600 dark:text-gray-300">Share the main's database</span>
        <Toggle
          on={Boolean(current.group_shared_db)}
          tone="violet"
          {loading}
          disabled={loading}
          title="Share the group main's database"
          onclick={() => toggleSharedDB(current)} />
      </div>
    </div>
    <div class="px-5 py-3 border-t border-gray-100 dark:border-lerd-border flex justify-end">
      <DetailButton tone="danger" onclick={unassignSelf} disabled={loading}>Ungroup this site</DetailButton>
    </div>
  {:else}
    {#if ungrouped}
      <div class="px-5 pt-4 flex gap-1.5">
        {#each [{ id: 'main', label: 'This is the main' }, { id: 'secondary', label: 'Secondary of…' }] as opt (opt.id)}
          <button
            onclick={() => (mode = opt.id as 'main' | 'secondary')}
            class="flex-1 text-xs font-medium px-3 py-1.5 rounded-sm border transition-colors {mode === opt.id
              ? 'border-lerd-red/40 bg-lerd-red/10 text-lerd-red'
              : 'border-gray-200 dark:border-lerd-border text-gray-600 dark:text-gray-400 hover:border-lerd-red/30'}"
          >
            {opt.label}
          </button>
        {/each}
      </div>
    {/if}

    {#if !ungrouped || mode === 'main'}
    <div class="px-5 py-4 space-y-2 max-h-64 overflow-y-auto">
      {#if current.multi_tenant}
        <div class="flex items-start gap-2 text-xs text-amber-700 dark:text-amber-400 bg-amber-50 dark:bg-amber-900/20 rounded-sm px-2 py-1.5">
          <Icon name="warn" class="w-4 h-4 shrink-0 mt-px" />
          <span>This site uses wildcard tenant subdomains. A grouped subdomain is carved out of that space and routed to the secondary instead.</span>
        </div>
      {/if}

      {#if secondaries.length === 0}
        <p class="text-sm text-gray-500 dark:text-gray-400">
          No secondaries yet. Add a site below to put it on a subdomain of
          <span class="font-mono">{current.domain}</span>.
        </p>
      {/if}

      {#each secondaries as sec (sec.domain)}
        <div class="flex items-center gap-2">
          {#if editDomain !== sec.domain}
            <div class="flex-1 min-w-0 flex items-center gap-1.5">
              <span class="text-sm font-mono text-gray-700 dark:text-gray-300 truncate">{sec.domain}</span>
              <span class="text-[10px] font-medium text-gray-500 dark:text-gray-400 truncate">{sec.name}</span>
            </div>
            <button
              onclick={() => toggleSharedDB(sec)}
              disabled={loading}
              class="text-[10px] font-medium px-1.5 py-0.5 rounded-sm shrink-0 transition-colors disabled:opacity-50 {sec.group_shared_db
                ? 'text-violet-700 dark:text-violet-300 bg-violet-50 dark:bg-violet-900/20'
                : 'text-gray-400 dark:text-gray-500 hover:text-violet-500'}"
              title={sec.group_shared_db ? 'Sharing the main database (click for separate)' : 'Separate database (click to share the main)'}
            >
              {sec.group_shared_db ? 'shared db' : 'own db'}
            </button>
            <button
              onclick={() => startEdit(sec)}
              class="text-gray-400 hover:text-lerd-red transition-colors"
              title="Edit subdomain"
              aria-label="Edit subdomain">
              <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z"/></svg>
            </button>
            <button
              onclick={() => remove(sec)}
              disabled={loading}
              class="text-gray-400 hover:text-red-500 transition-colors disabled:opacity-50"
              title="Remove from group"
              aria-label="Remove from group">
              <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg>
            </button>
          {:else}
            <input
              bind:value={editValue}
              onkeydown={(e) => {
                if (e.key === 'Enter') saveEdit(sec);
                if (e.key === 'Escape') cancelEdit();
              }}
              class="flex-1 text-sm font-mono bg-transparent border border-lerd-red/50 rounded-sm px-2 py-1 text-gray-700 dark:text-gray-300 focus:outline-hidden focus:border-lerd-red"
              disabled={loading} />
            <span class="text-sm text-gray-400 shrink-0">.{current.domain}</span>
            <button onclick={() => saveEdit(sec)} disabled={loading} class="text-emerald-500 hover:text-emerald-600 disabled:opacity-50" title="Save" aria-label="Save"><Icon name="check" class="w-4 h-4" /></button>
            <button onclick={cancelEdit} class="text-gray-400 hover:text-gray-600" title="Cancel" aria-label="Cancel"><Icon name="close" class="w-4 h-4" /></button>
          {/if}
        </div>
      {/each}
    </div>

    <div class="px-5 py-3 border-t border-gray-100 dark:border-lerd-border space-y-2">
      <div class="flex items-center gap-2">
        <Dropdown
          value={pickDomain}
          options={candidates}
          onchange={onPick}
          placeholder="Pick a site…"
          width="full"
          disabled={loading || candidates.length === 0} />
      </div>
      {#if pickDomain}
        <div class="flex items-center gap-2">
          <input
            bind:value={pickLabel}
            placeholder="subdomain"
            onkeydown={(e) => e.key === 'Enter' && addSecondary()}
            disabled={loading}
            class="flex-1 text-sm font-mono bg-transparent border border-gray-200 dark:border-lerd-border rounded-sm px-2 py-1.5 text-gray-700 dark:text-gray-300 placeholder-gray-400 dark:placeholder-gray-600 focus:outline-hidden focus:border-lerd-red/50" />
          <span class="text-sm text-gray-400 shrink-0">.{current.domain}</span>
          <DetailButton tone="primary" onclick={addSecondary} disabled={loading || !pickLabel.trim()}>Add</DetailButton>
        </div>
        <label class="flex items-center gap-2 text-xs text-gray-600 dark:text-gray-400 cursor-pointer select-none">
          <input type="checkbox" bind:checked={pickShareDB} disabled={loading} class="accent-lerd-red" />
          Share {current.name || 'main'}'s database instead of a separate one
        </label>
      {/if}
      {#if secondaries.length > 0}
        <div class="flex justify-end pt-1">
          <DetailButton tone="danger" onclick={dissolve} disabled={loading}>Dissolve group</DetailButton>
        </div>
      {/if}
    </div>
    {:else}
      <div class="px-5 py-4 space-y-2">
        <p class="text-sm text-gray-500 dark:text-gray-400">
          Put <span class="font-mono">{current.domain}</span> on a subdomain of another site.
        </p>
        <Dropdown
          value={pickMain}
          options={mainCandidates}
          onchange={onPickMain}
          placeholder="Pick the main site…"
          width="full"
          disabled={loading || mainCandidates.length === 0} />
        {#if pickMainSite}
          <div class="flex items-center gap-2">
            <input
              bind:value={pickLabel}
              placeholder="subdomain"
              onkeydown={(e) => e.key === 'Enter' && groupUnderMain()}
              disabled={loading}
              class="flex-1 text-sm font-mono bg-transparent border border-gray-200 dark:border-lerd-border rounded-sm px-2 py-1.5 text-gray-700 dark:text-gray-300 placeholder-gray-400 dark:placeholder-gray-600 focus:outline-hidden focus:border-lerd-red/50" />
            <span class="text-sm text-gray-400 shrink-0">.{pickMainSite.domain}</span>
            <DetailButton tone="primary" onclick={groupUnderMain} disabled={loading || !pickLabel.trim()}>Group</DetailButton>
          </div>
          <label class="flex items-center gap-2 text-xs text-gray-600 dark:text-gray-400 cursor-pointer select-none">
            <input type="checkbox" bind:checked={pickShareDB} disabled={loading} class="accent-lerd-red" />
            Share {pickMainSite.name || 'main'}'s database instead of a separate one
          </label>
        {/if}
      </div>
    {/if}
  {/if}

  {#if flash}
    <div class="px-5 py-2 border-t border-gray-100 dark:border-lerd-border">
      <p class="text-xs text-emerald-700 dark:text-emerald-500 bg-emerald-50 dark:bg-emerald-500/10 rounded-lg px-2 py-1.5 text-center">{flash}</p>
    </div>
  {/if}
  {#if error}
    <div class="px-5 py-2 border-t border-gray-100 dark:border-lerd-border">
      <p class="text-xs text-red-500">{error}</p>
    </div>
  {/if}
</Modal>
