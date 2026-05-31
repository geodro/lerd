<script lang="ts">
  import DetailPanel from '$components/DetailPanel.svelte';
  import PhpDetail from './PhpDetail.svelte';
  import { phpVersions } from '$stores/phpVersions';
  import { status } from '$stores/status';
  import { routeRest, goToTab } from '$stores/route';
  import { openPhpAddModal } from '$stores/modals';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    initialVersion?: string;
  }
  let { initialVersion = '' }: Props = $props();

  const phpDefault = $derived($status.php_default || '');

  function pickInitial(): string {
    if (initialVersion && $phpVersions.includes(initialVersion)) return initialVersion;
    if (phpDefault && $phpVersions.includes(phpDefault)) return phpDefault;
    return $phpVersions[0] ?? '';
  }

  let active = $state<string>(pickInitial());

  // Honour deep links like #system/php-8.3 and react when the available
  // versions change (e.g. after removing the currently active one).
  $effect(() => {
    const rest = $routeRest;
    if (rest.startsWith('php-')) {
      const v = rest.slice(4);
      if ($phpVersions.includes(v) && v !== active) active = v;
    }
  });

  // When the active version disappears (Remove action, manual rm, etc.) fall
  // back AND realign the URL — otherwise the hash keeps pointing at the
  // removed version while the page shows the fallback.
  $effect(() => {
    if (!active || !$phpVersions.includes(active)) {
      const next = pickInitial();
      if (next && next !== active) {
        active = next;
        goToTab('system', 'php-' + next);
      } else if (!next) {
        active = '';
        // No versions left: drop the stale php-<removed> hash so a reload or
        // shared link doesn't point at a version that no longer exists.
        if ($routeRest.startsWith('php-')) goToTab('system', '');
      }
    }
  });

  function pickVersion(v: string) {
    if (v === active) return;
    active = v;
    goToTab('system', 'php-' + v);
  }
</script>

<DetailPanel>
  <div class="flex items-end bg-gray-50/60 dark:bg-white/[0.02] border-b border-gray-100 dark:border-lerd-border shrink-0">
    <div class="flex items-center gap-0.5 px-3 pt-3 overflow-x-auto flex-1 min-w-0">
      {#each $phpVersions as v (v)}
        {@const isActive = v === active}
        {@const isDefault = v === phpDefault}
        {@const running = ($status.php_fpms.find((f) => f.version === v)?.running) ?? false}
        <button
          type="button"
          onclick={() => pickVersion(v)}
          title={'PHP ' + v + (isDefault ? ' (default)' : '')}
          class="group flex items-center gap-1.5 pl-3 pr-3 py-2.5 text-xs rounded-t-md border-t border-l border-r transition-colors max-w-56 shrink-0 {isActive
            ? 'bg-white dark:bg-lerd-bg border-gray-200 dark:border-lerd-border text-gray-800 dark:text-gray-100 font-medium'
            : 'bg-transparent border-transparent text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 hover:bg-gray-100/60 dark:hover:bg-white/5'}"
        >
          <span class="w-1.5 h-1.5 rounded-full shrink-0 {running ? 'bg-emerald-500' : 'bg-gray-300 dark:bg-gray-600'}"></span>
          <span class="font-mono leading-none">PHP {v}</span>
          {#if isDefault}
            <svg
              class="w-3 h-3 shrink-0 {isActive ? 'text-lerd-red' : 'text-amber-400 dark:text-amber-500'}"
              fill="currentColor"
              viewBox="0 0 20 20"
              aria-label="default"
            >
              <path d="M10 1.5l2.6 5.27 5.82.85-4.21 4.1.99 5.78L10 14.77l-5.2 2.73.99-5.78L1.58 7.62l5.82-.85L10 1.5z" />
            </svg>
          {/if}
        </button>
      {/each}
      <button
        type="button"
        onclick={() => openPhpAddModal()}
        class="ml-1 mb-0.5 w-6 h-6 flex items-center justify-center rounded-md text-gray-400 hover:text-lerd-red hover:bg-gray-100 dark:hover:bg-white/5 transition-colors shrink-0"
        title={m.system_php_add()}
        aria-label={m.system_php_add()}
      >
        <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
        </svg>
      </button>
    </div>
  </div>

  {#if active}
    <PhpDetail version={active} />
  {/if}
</DetailPanel>
