<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import type { Site } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    open: boolean;
    site: Site;
    branch: string;
    onclose: () => void;
    onconfirm: (source: string) => void;
  }
  let { open, site, branch, onclose, onconfirm }: Props = $props();

  type Option = { value: string; label: string };

  const options = $derived.by<Option[]>(() => {
    const opts: Option[] = [
      { value: 'main', label: m.worktreeDb_cloneFromMain({ domain: site.domain }) },
      { value: '', label: m.worktreeDb_empty() }
    ];
    for (const w of site.worktrees || []) {
      if (w.db_isolated && w.branch && w.branch !== branch) {
        opts.push({
          value: w.branch,
          label: w.db_database
            ? m.worktreeDb_cloneFromBranchDb({ branch: w.branch, db: w.db_database })
            : m.worktreeDb_cloneFromBranch({ branch: w.branch })
        });
      }
    }
    return opts;
  });

  let selected = $state('main');

  $effect(() => {
    if (open) selected = 'main';
  });

  function confirm() {
    onconfirm(selected);
    onclose();
  }
</script>

<Modal {open} {onclose} title={m.worktreeDb_title()} size="sm">
  <div class="px-5 py-4 space-y-3">
    <p class="text-sm text-gray-600 dark:text-gray-400">
      {m.worktreeDb_body({ branch })}
    </p>
    <select
      bind:value={selected}
      class="w-full text-sm bg-white dark:bg-lerd-bg border border-gray-200 dark:border-lerd-border rounded-sm px-2 py-1.5 text-gray-700 dark:text-gray-300 hover:border-gray-300 dark:hover:border-lerd-muted focus:outline-hidden focus:border-lerd-red/50 cursor-pointer transition-colors"
    >
      {#each options as opt (opt.value)}
        <option value={opt.value} class="bg-white text-gray-700 dark:bg-lerd-bg dark:text-gray-300">{opt.label}</option>
      {/each}
    </select>
    <p class="text-[11px] text-gray-400 dark:text-gray-500">
      {m.worktreeDb_cloningHint()}
    </p>
  </div>
  {#snippet footer()}
    <button
      type="button"
      onclick={onclose}
      class="text-xs px-3 py-1.5 rounded-sm border border-gray-200 dark:border-lerd-border text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-white/5 transition-colors"
    >{m.common_cancel()}</button>
    <button
      type="button"
      onclick={confirm}
      class="text-xs px-3 py-1.5 rounded-sm bg-lerd-red hover:bg-lerd-redhov text-white transition-colors"
    >{m.worktreeDb_isolateAction()}</button>
  {/snippet}
</Modal>
