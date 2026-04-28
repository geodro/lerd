<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import { closeModal } from '$stores/modals';
  import { remoteControl, enableRemoteControl } from '$stores/remoteControl';
  import { m } from '../paraglide/messages.js';

  let username = $state('');
  let password = $state('');
  let error = $state('');

  async function confirm() {
    if (!username || !password) return;
    error = '';
    const r = await enableRemoteControl(username, password);
    if (r.ok) {
      closeModal();
    } else {
      error = r.error || m.common_failed();
    }
  }
</script>

<Modal open title={m.system_remoteModal_title()} onclose={closeModal}>
  <div class="px-5 py-4 space-y-3">
    <p class="text-xs text-gray-500 dark:text-gray-400">{m.system_remoteModal_subtitle()}</p>
    <div class="space-y-2">
      <input
        type="text"
        bind:value={username}
        placeholder={m.system_remoteModal_username()}
        autocomplete="off"
        class="w-full text-sm bg-gray-50 dark:bg-lerd-bg border border-gray-200 dark:border-lerd-border rounded px-2.5 py-1.5 text-gray-700 dark:text-gray-300 placeholder-gray-400 focus:outline-none focus:border-lerd-red/50"
      />
      <input
        type="password"
        bind:value={password}
        placeholder={m.system_remoteModal_password()}
        autocomplete="new-password"
        onkeydown={(e) => e.key === 'Enter' && confirm()}
        class="w-full text-sm bg-gray-50 dark:bg-lerd-bg border border-gray-200 dark:border-lerd-border rounded px-2.5 py-1.5 text-gray-700 dark:text-gray-300 placeholder-gray-400 focus:outline-none focus:border-lerd-red/50"
      />
    </div>
    {#if error}
      <p class="text-xs text-red-500">{error}</p>
    {/if}
  </div>

  {#snippet footer()}
    <DetailButton onclick={closeModal}>{m.common_cancel()}</DetailButton>
    <DetailButton
      tone="primary"
      onclick={confirm}
      disabled={$remoteControl.loading || !username || !password}
      loading={$remoteControl.loading}
    >{$remoteControl.loading ? m.system_remoteModal_enabling() : m.system_remoteModal_enable()}</DetailButton>
  {/snippet}
</Modal>
