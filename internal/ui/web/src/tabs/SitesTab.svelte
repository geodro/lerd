<script lang="ts">
  import ListPanel from '$components/ListPanel.svelte';
  import ActionButton from '$components/ActionButton.svelte';
  import DumpBridgeToggle from '$components/DumpBridgeToggle.svelte';
  import ProfilerToggle from '$components/ProfilerToggle.svelte';
  import EmptyState from '$components/EmptyState.svelte';
  import Icon from '$components/Icon.svelte';
  import SiteIcon from '$components/SiteIcon.svelte';
  import SiteIndicators from '$components/SiteIndicators.svelte';
  import LoadingRow from '$components/LoadingRow.svelte';
  import { accessMode } from '$stores/accessMode';
  import { routeRest, goToTab } from '$stores/route';
  import { sites, sitesLoaded, reorderSites, type Site } from '$stores/sites';
  import { sitesSort, type SitesSort } from '$stores/sitesSort';
  import { openLinkModal } from '$stores/modals';
  import { get } from 'svelte/store';
  import { flushSync, untrack } from 'svelte';
  import { dndzone, SOURCES, TRIGGERS, type DndEvent } from 'svelte-dnd-action';
  import { flip } from 'svelte/animate';
  import { m } from '../paraglide/messages.js';

  const selected = $derived($routeRest);
  const active = $derived($sites.filter((s) => !s.paused));
  const paused = $derived($sites.filter((s) => s.paused));

  // The active secondaries pinned under a main, drawn from the given list. Single
  // source of truth for the grouping rule, used both for rendering and persisting.
  function secondariesOf(list: Site[], main: Site): Site[] {
    if (!main.group) return [];
    return list.filter((x) => !x.paused && x.group === main.group && x.group_subdomain);
  }
  function secondariesFor(s: Site): Site[] {
    return secondariesOf(active, s);
  }

  // The main (non-secondary) active rows, ordered per the chosen sort mode.
  // Only mains are sorted; secondaries always stay pinned under their main.
  const mains = $derived(active.filter((s) => !s.group_subdomain));
  const sortedMains = $derived.by(() => {
    const list = [...mains];
    switch ($sitesSort) {
      case 'alpha':
        return list.sort((a, b) => a.domain.localeCompare(b.domain));
      case 'recent':
        // Newest activity first; sites with no log activity sink to the bottom.
        return list.sort((a, b) => (b.latest_log_time || '').localeCompare(a.latest_log_time || ''));
      case 'newest':
        return list.reverse();
      case 'manual':
      default:
        return list;
    }
  });
  // Reordering is available whenever we can write (loopback), in any sort mode.
  // Dragging a site auto-switches the list into manual mode (see applyDrop).
  const canReorder = $derived($accessMode.loopback);

  // Active secondaries whose main isn't active (e.g. the main is paused) render
  // on their own below the draggable list so they never disappear.
  const orphanSecondaries = $derived.by(() => {
    const covered = new Set<string>();
    for (const main of sortedMains) for (const sec of secondariesFor(main)) covered.add(sec.domain);
    return active.filter((s) => s.group_subdomain && !covered.has(s.domain));
  });

  // Drag-and-drop via svelte-dnd-action. Each item is a main row carrying its
  // secondaries, so a whole group moves and animates together. Dragging flips
  // the list into manual mode, seeded from whatever order is shown at drag start.
  type DndItem = { id: string; site: Site };
  const FLIP_MS = 180;
  // Unique per instance so the desktop and mobile lists aren't connected zones.
  const dndType = 'sites-' + Math.random().toString(36).slice(2, 9);
  let dragDisabled = $state(true);
  let dndItems = $state<DndItem[]>([]);
  $effect(() => {
    // Resync from the sorted source whenever we're not mid-drag. When only the
    // row data changed (same order, e.g. a live status push) refresh in place so
    // the dnd zone isn't rebuilt on every snapshot; rebuild only when the order
    // actually changed (sort switch or an external reorder).
    if (!dragDisabled) return;
    const sorted = sortedMains;
    // Read/write dndItems untracked so refreshing it here never feeds back into
    // this effect (its only deps are sortedMains and dragDisabled).
    untrack(() => {
      const cur = dndItems;
      const sameOrder = sorted.length === cur.length && sorted.every((s, i) => s.domain === cur[i].id);
      if (sameOrder) {
        sorted.forEach((s, i) => {
          if (cur[i].site !== s) cur[i].site = s;
        });
      } else {
        dndItems = sorted.map((s) => ({ id: s.domain, site: s }));
      }
    });
  });

  function startDrag(e: MouseEvent | TouchEvent) {
    if (e instanceof MouseEvent && e.button !== 0) return;
    e.preventDefault();
    dragDisabled = false;
    // svelte-dnd-action only attaches its drag listener while enabled; flush the
    // state change now so the listener catches this same press as it bubbles up.
    flushSync();
  }

  // Real (non-delegated) listener on the handle. Svelte 5 delegates mousedown to
  // the document root, which would run after the event already passed the item;
  // attaching directly here lets flushSync wire up the drag before it bubbles up.
  function dragHandle(node: HTMLElement) {
    const onDown = (e: Event) => startDrag(e as MouseEvent | TouchEvent);
    node.addEventListener('mousedown', onDown);
    node.addEventListener('touchstart', onDown, { passive: false });
    return {
      destroy() {
        node.removeEventListener('mousedown', onDown);
        node.removeEventListener('touchstart', onDown);
      }
    };
  }
  function handleConsider(e: CustomEvent<DndEvent<DndItem>>) {
    dndItems = e.detail.items;
    const { source, trigger } = e.detail.info;
    if (source === SOURCES.KEYBOARD && trigger === TRIGGERS.DRAG_STOPPED) dragDisabled = true;
  }
  function handleFinalize(e: CustomEvent<DndEvent<DndItem>>) {
    dndItems = e.detail.items;
    if (e.detail.info.source === SOURCES.POINTER) dragDisabled = true;
    persistOrder(e.detail.items.map((i) => i.site));
  }

  async function persistOrder(newMains: Site[]) {
    const prevSites = get(sites);
    const prevSort = get(sitesSort);
    const all = prevSites;
    const byDomain = new Map(all.map((s) => [s.domain, s]));
    const next: Site[] = [];
    const used = new Set<string>();
    for (const m of newMains) {
      const main = byDomain.get(m.domain);
      if (!main || used.has(main.domain)) continue;
      next.push(main);
      used.add(main.domain);
      for (const sec of secondariesOf(all, main)) {
        if (!used.has(sec.domain)) {
          next.push(sec);
          used.add(sec.domain);
        }
      }
    }
    for (const s of all) if (!used.has(s.domain)) next.push(s);

    sitesSort.set('manual'); // dragging is what enables manual ordering
    sites.set(next); // optimistic; the KindSites WS push reconciles to server truth
    // Revert the optimistic order if the server rejected it, instead of leaving
    // an order on screen that was never saved.
    const res = await reorderSites(next.map((s) => s.name).filter((n): n is string => Boolean(n)));
    if (!res.ok) {
      sites.set(prevSites);
      sitesSort.set(prevSort);
      console.error('reorder failed:', res.error);
    }
  }

  function select(s: Site) {
    goToTab('sites', s.domain);
  }

  const sortOptions: Array<{ value: SitesSort; label: string }> = $derived([
    { value: 'recent', label: m.sites_sort_recent() },
    { value: 'alpha', label: m.sites_sort_alpha() },
    { value: 'newest', label: m.sites_sort_newest() }
  ]);

  let sortMenuOpen = $state(false);
  let sortRootEl: HTMLDivElement | undefined = $state();

  function pickSort(v: SitesSort) {
    sitesSort.set(v);
    sortMenuOpen = false;
  }
  function onSortDocClick(e: MouseEvent) {
    if (sortRootEl && !sortRootEl.contains(e.target as Node)) sortMenuOpen = false;
  }
  function onSortKey(e: KeyboardEvent) {
    if (e.key === 'Escape') sortMenuOpen = false;
  }
  $effect(() => {
    if (!sortMenuOpen) return;
    document.addEventListener('mousedown', onSortDocClick);
    document.addEventListener('keydown', onSortKey);
    return () => {
      document.removeEventListener('mousedown', onSortDocClick);
      document.removeEventListener('keydown', onSortKey);
    };
  });
</script>

{#snippet actions()}
  {#if $accessMode.loopback}
    <DumpBridgeToggle />
    <ProfilerToggle />
    <ActionButton title={m.sites_linkNew()} tone="accent" onclick={openLinkModal}>
      <Icon name="plus" class="w-3.5 h-3.5" />
    </ActionButton>
  {/if}
{/snippet}

{#snippet parkHint()}
  {@html m.sites_emptyHint({ cmd: '<code class="bg-gray-100 dark:bg-white/5 px-1 rounded-sm">lerd park</code>' })}
{/snippet}

{#snippet sortOverlay()}
  <div bind:this={sortRootEl} class="absolute bottom-3 right-3 z-20">
    {#if sortMenuOpen}
      <div
        role="menu"
        class="absolute bottom-full right-0 mb-2 min-w-44 rounded-lg border border-gray-200 dark:border-lerd-border bg-white dark:bg-lerd-card shadow-xl py-1"
      >
        {#each sortOptions as opt (opt.value)}
          <button
            type="button"
            role="menuitemradio"
            aria-checked={$sitesSort === opt.value}
            onclick={() => pickSort(opt.value)}
            class="w-full flex items-center gap-2 px-3 py-1.5 text-left text-xs transition-colors hover:bg-gray-50 dark:hover:bg-white/5 {$sitesSort ===
            opt.value
              ? 'text-lerd-red font-semibold'
              : 'text-gray-700 dark:text-gray-200'}"
          >
            <span class="w-3 h-3 shrink-0">
              {#if $sitesSort === opt.value}<Icon name="check" class="w-3 h-3" />{/if}
            </span>
            <span class="flex-1">{opt.label}</span>
          </button>
        {/each}
      </div>
    {/if}
    <button
      type="button"
      onclick={() => (sortMenuOpen = !sortMenuOpen)}
      title={m.sites_sort_label()}
      aria-haspopup="menu"
      aria-expanded={sortMenuOpen}
      aria-label={m.sites_sort_label()}
      class="flex items-center justify-center w-9 h-9 rounded-full bg-gray-100 dark:bg-white/10 border border-gray-200 dark:border-white/15 backdrop-blur-sm text-gray-800 dark:text-gray-200 hover:border-lerd-red hover:text-lerd-red transition-colors"
    >
      <Icon name="sort" class="w-4 h-4" />
    </button>
  </div>
{/snippet}

{#snippet siteRow(s: Site, grouped = false)}
  <button
    onclick={() => select(s)}
    class="group relative w-full flex items-center gap-2 px-3 py-2.5 text-left transition-colors border-b border-gray-50 dark:border-lerd-border/50 {selected ===
    s.domain
      ? 'bg-lerd-red/10 text-lerd-red'
      : 'text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-white/3'}"
  >
    {#if canReorder && !grouped}
      <span
        role="button"
        tabindex="-1"
        aria-label={m.sites_sort_reorder()}
        use:dragHandle
        onclick={(e) => e.stopPropagation()}
        onkeydown={(e) => e.stopPropagation()}
        class="absolute left-2 top-1/2 -translate-y-1/2 z-10 flex items-center justify-center w-6 h-6 rounded-md border border-gray-200 dark:border-white/15 bg-gray-100 dark:bg-lerd-card text-gray-800 dark:text-gray-200 hover:text-lerd-red hover:border-lerd-red cursor-grab active:cursor-grabbing opacity-0 group-hover:opacity-100 transition-opacity"
      >
        <Icon name="grip" class="w-4 h-4" />
      </span>
    {/if}
    {#if grouped}
      <Icon name="group" class="w-3.5 h-3.5 shrink-0 text-gray-400 dark:text-gray-500" />
    {/if}
    <span class="relative shrink-0 w-4 h-4 flex items-center justify-center">
      <SiteIcon site={s} />
    </span>
    <span class="flex-1 text-sm truncate">{s.domain}</span>
    {#if s.tls}
      <svg class="w-3 h-3 shrink-0 text-emerald-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z"/>
      </svg>
    {/if}
    <SiteIndicators site={s} />
  </button>
{/snippet}

<ListPanel title={m.sites_title()} {actions} overlay={$sitesLoaded && $sites.length > 0 ? sortOverlay : undefined}>
  {#if !$sitesLoaded}
    <LoadingRow />
  {:else if $sites.length === 0}
    <EmptyState title={m.sites_empty()} hint={parkHint} size="sm" />
  {:else}
    <section
      use:dndzone={{ items: dndItems, type: dndType, flipDurationMs: FLIP_MS, dragDisabled, dropTargetStyle: {} }}
      onconsider={handleConsider}
      onfinalize={handleFinalize}
    >
      {#each dndItems as item (item.id)}
        <div animate:flip={{ duration: FLIP_MS }}>
          {@render siteRow(item.site, false)}
          {#each secondariesFor(item.site) as sec (sec.domain)}
            {@render siteRow(sec, true)}
          {/each}
        </div>
      {/each}
    </section>

    {#each orphanSecondaries as s (s.domain)}
      {@render siteRow(s, true)}
    {/each}

    {#if paused.length > 0}
      <div class="border-t border-gray-100 dark:border-lerd-border">
        <div class="px-3 py-1.5 text-[10px] font-semibold uppercase tracking-wider text-gray-400 dark:text-gray-500">{m.sites_paused()}</div>
        {#each paused as s (s.domain)}
          <button
            onclick={() => select(s)}
            class="w-full flex items-center gap-2 px-3 py-2 text-left transition-colors border-t border-gray-50 dark:border-lerd-border/50 {selected === s.domain
              ? 'bg-lerd-red/10 text-lerd-red'
              : 'text-gray-400 dark:text-gray-500 hover:bg-gray-50 dark:hover:bg-white/3'}"
          >
            <svg class="w-3 h-3 shrink-0 opacity-60" fill="currentColor" viewBox="0 0 24 24">
              <path d="M6 5h4v14H6zM14 5h4v14h-4z"/>
            </svg>
            <span class="flex-1 text-sm truncate">{s.domain}</span>
          </button>
        {/each}
      </div>
    {/if}
  {/if}
</ListPanel>
