<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import Modal from '$components/Modal.svelte';
  import Dropdown from '$components/Dropdown.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import BuildLog from '$components/BuildLog.svelte';
  import { closeModal } from '$stores/modals';
  import { installablePhpVersions, streamPhpInstall, loadPhpVersions } from '$stores/phpVersions';
  import { loadStatus } from '$stores/status';
  import { goToTab } from '$stores/route';
  import { m } from '../paraglide/messages.js';

  let versions = $state<string[]>([]);
  let optsLoading = $state(true);
  let optsError = $state('');
  let version = $state('');
  let installing = $state(false);
  let finished = $state(false);
  let error = $state('');
  let logs = $state<string[]>([]);

  // Cap retained log lines so a long first-time build doesn't grow the array
  // (and re-render cost) without bound; the tail is what matters.
  const MAX_LOG_LINES = 1000;

  // The build keeps running server-side after the modal closes; `alive` lets the
  // completion handler skip UI nav once unmounted, and aborting the controller
  // stops the client read so we don't keep mutating dead state for the whole
  // build. A push notification (op_done / op_failed) reports the result instead.
  let alive = true;
  let controller: AbortController | null = null;
  onDestroy(() => {
    alive = false;
    controller?.abort();
  });

  const canInstall = $derived(!installing && version !== '');

  async function loadVersions() {
    optsLoading = true;
    optsError = '';
    try {
      const list = await installablePhpVersions();
      versions = list;
      version = list[0] ?? '';
    } catch (e) {
      optsError = e instanceof Error ? e.message : m.common_failed();
    } finally {
      optsLoading = false;
    }
  }

  async function install() {
    if (!version) return;
    installing = true;
    finished = false;
    error = '';
    logs = [];
    const target = version;
    controller = new AbortController();
    try {
      const box: { done: boolean; ok: boolean; error?: string } = { done: false, ok: false };
      await streamPhpInstall(
        target,
        (ev) => {
          if (!alive) return;
          if (ev.done) {
            box.done = true;
            box.ok = Boolean(ev.ok);
            box.error = ev.error;
            return;
          }
          if (ev.line !== undefined) {
            logs = [...logs, ev.line].slice(-MAX_LOG_LINES);
          }
        },
        controller.signal
      );
      if (!alive) return;
      await loadPhpVersions();
      await loadStatus();
      // Stream ended without a final result (e.g. connection dropped): don't
      // assert failure. The refreshed list/status and the push notification
      // report the real outcome; just close.
      if (!box.done) {
        closeModal();
        return;
      }
      if (!box.ok) {
        error = box.error || m.system_php_installFailed();
        finished = true;
        return;
      }
      goToTab('system', 'php-' + target);
      closeModal();
    } catch (e) {
      if (!alive) return;
      error = e instanceof Error ? e.message : m.system_php_installFailed();
      finished = true;
    } finally {
      if (alive) installing = false;
    }
  }

  onMount(() => {
    void loadVersions();
  });
</script>

<Modal open title={m.system_php_add()} onclose={closeModal} size="lg">
  {#if !installing && !finished}
    <div class="px-5 py-4 space-y-4">
      <p class="text-xs text-gray-500 dark:text-gray-400">{m.system_php_addHint()}</p>
      {#if optsLoading}
        <p class="text-sm text-gray-400">…</p>
      {:else if optsError}
        <div class="rounded-lg border border-red-200 dark:border-red-500/30 bg-red-50 dark:bg-red-500/10 px-3 py-2 text-xs text-red-700 dark:text-red-300">
          {optsError}
        </div>
      {:else if versions.length === 0}
        <p class="text-sm text-gray-500 dark:text-gray-400">{m.system_php_allInstalled()}</p>
      {:else}
        <div class="space-y-1.5">
          <div class="text-xs font-medium text-gray-500 dark:text-gray-400">{m.system_php_addVersion()}</div>
          <Dropdown
            value={version}
            width="full"
            placeholder={m.system_php_addPlaceholder()}
            options={versions.map((v) => ({ value: v, label: 'PHP ' + v }))}
            onchange={(val) => (version = val)}
          />
        </div>
      {/if}
    </div>
  {:else}
    <div class="px-5 py-3 space-y-2">
      {#if finished && error}
        <div class="rounded-lg border border-red-200 dark:border-red-500/30 bg-red-50 dark:bg-red-500/10 px-3 py-2 text-xs text-red-700 dark:text-red-300">
          {error}
        </div>
      {/if}
      <BuildLog {logs} />
    </div>
  {/if}

  {#snippet footer()}
    {#if finished}
      <DetailButton tone="primary" onclick={closeModal}>{m.common_close()}</DetailButton>
    {:else if !installing}
      <DetailButton onclick={closeModal}>{m.common_cancel()}</DetailButton>
      <DetailButton
        tone="primary"
        onclick={install}
        disabled={!canInstall || optsLoading || versions.length === 0}
        loading={installing}
      >
        {m.system_php_install()}
      </DetailButton>
    {:else}
      <DetailButton tone="primary" disabled loading={true}>{m.system_php_installing()}</DetailButton>
    {/if}
  {/snippet}
</Modal>
