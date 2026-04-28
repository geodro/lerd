<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import Icon from '$components/Icon.svelte';
  import { closeModal } from '$stores/modals';
  import { loadSites, sites, type Site } from '$stores/sites';
  import { status } from '$stores/status';
  import { addDomain, editDomain, removeDomain } from '$stores/domains';
  import { m } from '../paraglide/messages.js';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();

  const tld = $derived($status.dns.tld || 'test');
  const suffix = $derived('.' + tld);

  // Reactively track the latest site record. Match by name first so we survive
  // primary-domain renames; fall back to the initial domain.
  const current = $derived(
    $sites.find((s) => site.name && s.name === site.name) ??
      $sites.find((s) => s.domain === site.domain) ??
      site
  );

  const domains = $derived(
    (current.domains || [current.domain]).map((d) =>
      d.endsWith(suffix) ? d.slice(0, -suffix.length) : d
    )
  );
  const conflicting = $derived(current.conflicting_domains || []);

  let newDomain = $state('');
  let editIndex = $state(-1);
  let editValue = $state('');
  let loading = $state(false);
  let error = $state('');
  let flash = $state('');
  let flashTimer: ReturnType<typeof setTimeout> | null = null;

  function showFlash(msg: string) {
    flash = msg;
    if (flashTimer) clearTimeout(flashTimer);
    flashTimer = setTimeout(() => (flash = ''), 3000);
  }

  async function runAction(fn: () => Promise<{ ok: boolean; error?: string }>, successMsg: string) {
    loading = true;
    error = '';
    try {
      const r = await fn();
      if (!r.ok) {
        error = r.error || m.common_failed();
        return;
      }
      await loadSites();
      showFlash(successMsg);
    } finally {
      loading = false;
    }
  }

  function startEdit(i: number) {
    editIndex = i;
    editValue = domains[i];
  }
  function cancelEdit() {
    editIndex = -1;
    editValue = '';
  }
  async function saveEdit(i: number) {
    const oldName = domains[i];
    const newName = editValue.trim().toLowerCase();
    if (!newName || newName === oldName) {
      cancelEdit();
      return;
    }
    await runAction(() => editDomain(current, oldName, newName), m.domains_flash_updated());
    if (!error) cancelEdit();
  }
  async function add() {
    const name = newDomain.trim().toLowerCase();
    if (!name) return;
    await runAction(() => addDomain(current, name), m.domains_flash_added());
    if (!error) newDomain = '';
  }
  async function remove(name: string) {
    if (domains.length <= 1) {
      error = m.domains_cannotRemoveLast();
      return;
    }
    await runAction(() => removeDomain(current, name), m.domains_flash_removed());
  }
  async function removeConflict(fullDomain: string) {
    const nameOnly = fullDomain.endsWith(suffix) ? fullDomain.slice(0, -suffix.length) : fullDomain;
    await runAction(() => removeDomain(current, nameOnly), m.domains_flash_removedYaml());
  }
</script>

<Modal open title={m.domains_title()} onclose={closeModal}>
  <div class="px-5 py-4 space-y-2 max-h-64 overflow-y-auto">
    {#each conflicting as c (c.domain)}
      <div class="flex items-center gap-2 group opacity-70">
        <svg class="w-4 h-4 text-amber-500 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01M4.93 19h14.14a2 2 0 001.74-3l-7.07-12a2 2 0 00-3.48 0L3.19 16a2 2 0 001.74 3z"/>
        </svg>
        <div class="flex-1 min-w-0 flex items-center gap-1.5">
          <span class="text-sm font-mono text-gray-500 dark:text-gray-400 truncate line-through">{c.domain}</span>
          {#if c.owned_by}
            <span class="text-[10px] font-medium text-amber-700 dark:text-amber-400 bg-amber-50 dark:bg-amber-900/20 px-1.5 py-0.5 rounded shrink-0">{m.domains_conflict_usedBy({ owner: c.owned_by })}</span>
          {/if}
        </div>
        <button
          onclick={() => removeConflict(c.domain)}
          disabled={loading}
          class="text-gray-400 hover:text-red-500 transition-colors disabled:opacity-50"
          title={m.domains_conflict_removeYaml()}
        >
          <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/>
          </svg>
        </button>
      </div>
    {/each}

    {#each domains as dom, i (dom + ':' + i)}
      <div class="flex items-center gap-2">
        {#if editIndex !== i}
          <div class="flex-1 min-w-0 flex items-center gap-1.5">
            <span class="text-sm font-mono text-gray-700 dark:text-gray-300 truncate">{dom}</span>
            <span class="text-sm text-gray-400 dark:text-gray-500 shrink-0">.{tld}</span>
            {#if i === 0}
              <span class="text-[10px] font-medium text-lerd-red bg-red-50 dark:bg-red-900/20 px-1.5 py-0.5 rounded shrink-0">{m.domains_primary()}</span>
            {/if}
          </div>
          <button
            onclick={() => startEdit(i)}
            class="text-gray-400 hover:text-lerd-red transition-colors"
            title={m.common_edit()}
          >
            <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z"/>
            </svg>
          </button>
          {#if domains.length > 1}
            <button
              onclick={() => remove(dom)}
              disabled={loading}
              class="text-gray-400 hover:text-red-500 transition-colors disabled:opacity-50"
              title={m.common_remove()}
            >
              <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/>
              </svg>
            </button>
          {/if}
        {:else}
          <input
            bind:value={editValue}
            onkeydown={(e) => {
              if (e.key === 'Enter') saveEdit(i);
              if (e.key === 'Escape') cancelEdit();
            }}
            class="flex-1 text-sm font-mono bg-transparent border border-lerd-red/50 rounded px-2 py-1 text-gray-700 dark:text-gray-300 focus:outline-none focus:border-lerd-red"
            disabled={loading}
          />
          <span class="text-sm text-gray-400 shrink-0">.{tld}</span>
          <button onclick={() => saveEdit(i)} disabled={loading} class="text-emerald-500 hover:text-emerald-600 disabled:opacity-50" title={m.common_save()}>
            <Icon name="check" class="w-4 h-4" />
          </button>
          <button onclick={cancelEdit} class="text-gray-400 hover:text-gray-600" title={m.common_cancel()}>
            <Icon name="close" class="w-4 h-4" />
          </button>
        {/if}
      </div>
    {/each}
  </div>

  <div class="px-5 py-3 border-t border-gray-100 dark:border-lerd-border">
    <div class="flex items-center gap-2">
      <input
        type="text"
        bind:value={newDomain}
        placeholder={m.domains_add()}
        onkeydown={(e) => e.key === 'Enter' && add()}
        disabled={loading}
        class="flex-1 text-sm font-mono bg-transparent border border-gray-200 dark:border-lerd-border rounded px-2 py-1.5 text-gray-700 dark:text-gray-300 placeholder-gray-400 dark:placeholder-gray-600 focus:outline-none focus:border-lerd-red/50"
      />
      <span class="text-sm text-gray-400 shrink-0">.{tld}</span>
      <DetailButton tone="primary" onclick={add} disabled={loading || !newDomain.trim()}>{m.common_add()}</DetailButton>
    </div>
  </div>

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
