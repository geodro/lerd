<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import { type Service, setServicePorts } from '$stores/services';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    open: boolean;
    svc: Service;
    onclose: () => void;
  }
  let { open, svc, onclose }: Props = $props();

  const isBuiltin = $derived(Boolean(svc.is_default));

  // Shared with the domains modal's add inputs so the two read identically.
  const inputCls =
    'text-sm tabular-nums bg-transparent border border-gray-200 dark:border-lerd-border rounded-sm px-2 py-1.5 text-gray-700 dark:text-gray-300 placeholder-gray-400 dark:placeholder-gray-600 focus:outline-hidden focus:border-lerd-red/50 transition-colors disabled:opacity-50';

  // Number inputs bound with bind:value yield number | null (null when empty),
  // so these stay numeric — never strings.
  let publishedInput = $state<number | null>(null);
  let extraPorts = $state<string[]>([]);
  let newHost = $state<number | null>(null);
  let newContainer = $state<number | null>(null);
  let saving = $state(false);
  let error = $state('');

  // Reseed from the service every time the modal opens so a reopen never shows
  // stale edits from a cancelled session.
  $effect(() => {
    if (open) {
      publishedInput = svc.published_port || svc.default_port || null;
      extraPorts = [...(svc.extra_ports ?? [])];
      newHost = null;
      newContainer = null;
      saving = false;
      error = '';
    }
  });

  function validPort(n: number | null): n is number {
    return n != null && Number.isInteger(n) && n >= 0 && n <= 65535;
  }

  // Assemble the two number fields into a single "host:container" mapping, the
  // form the backend, CLI and config.yaml all use.
  function addExtra() {
    if (!validPort(newHost) || !validPort(newContainer)) {
      error = m.services_ports_invalidPort();
      return;
    }
    const spec = newHost + ':' + newContainer;
    if (!extraPorts.includes(spec)) extraPorts = [...extraPorts, spec];
    newHost = null;
    newContainer = null;
    error = '';
  }

  function removeExtra(spec: string) {
    extraPorts = extraPorts.filter((p) => p !== spec);
  }

  async function save() {
    error = '';
    let published: number | null = null;
    if (publishedInput != null) {
      if (!validPort(publishedInput)) {
        error = m.services_ports_invalidPort();
        return;
      }
      // The preset default isn't an override, so saving it leaves the field
      // unset and keeps the auto-shift guard on.
      published = svc.default_port && publishedInput === svc.default_port ? null : publishedInput;
    }
    saving = true;
    try {
      const res = await setServicePorts(svc.name, {
        published_port: published,
        extra_ports: isBuiltin ? extraPorts : []
      });
      if (!res.ok) {
        error = res.error || m.common_failed();
        return;
      }
      onclose();
    } finally {
      saving = false;
    }
  }
</script>

<Modal {open} {onclose} title={m.services_ports_title({ name: svc.name })} size="md">
  <div class="px-5 py-4 space-y-5">
    <div class="space-y-2">
      <div>
        <span class="text-sm font-medium text-gray-800 dark:text-gray-200">
          {m.services_ports_publishedLabel()}
        </span>
        <p class="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
          {m.services_ports_publishedHelp({ name: svc.name })}
        </p>
      </div>
      <div class="flex items-center gap-3">
        <input
          type="number"
          min="0"
          max="65535"
          bind:value={publishedInput}
          placeholder={svc.default_port ? String(svc.default_port) : ''}
          onkeydown={(e) => e.key === 'Enter' && save()}
          disabled={saving}
          class="w-32 {inputCls}"
        />
        {#if svc.default_port}
          <span class="text-xs text-gray-500 dark:text-gray-400">
            {m.services_ports_defaultHint({ port: svc.default_port })}
          </span>
          <button
            type="button"
            onclick={() => (publishedInput = svc.default_port ?? null)}
            disabled={publishedInput === svc.default_port}
            class="ml-auto text-xs text-gray-500 dark:text-gray-400 hover:text-lerd-red transition-colors disabled:opacity-40 disabled:hover:text-gray-500 dark:disabled:hover:text-gray-400"
          >{m.services_ports_resetDefault()}</button>
        {/if}
      </div>
    </div>

    <div class="space-y-2 border-t border-gray-100 dark:border-lerd-border pt-4">
      <span class="text-sm font-medium text-gray-800 dark:text-gray-200">
        {m.services_ports_extraTitle()}
      </span>
      {#if !isBuiltin}
        <p class="text-xs text-gray-500 dark:text-gray-400">{m.services_ports_extraPresetOnly()}</p>
      {:else}
        <p class="text-xs text-gray-500 dark:text-gray-400">{m.services_ports_extraHelp()}</p>
        {#if extraPorts.length === 0}
          <p class="text-xs text-gray-400 dark:text-gray-500 italic">{m.services_ports_extraEmpty()}</p>
        {:else}
          <div class="space-y-1.5">
            {#each extraPorts as spec (spec)}
              <div class="flex items-center gap-2">
                <div class="flex-1 min-w-0 flex items-center gap-1.5">
                  <span class="text-sm font-mono text-gray-700 dark:text-gray-300 truncate">{spec}</span>
                </div>
                <button
                  type="button"
                  onclick={() => removeExtra(spec)}
                  disabled={saving}
                  title={m.common_remove()}
                  class="text-gray-400 hover:text-red-500 transition-colors disabled:opacity-50"
                >
                  <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/>
                  </svg>
                </button>
              </div>
            {/each}
          </div>
        {/if}
        <div class="flex items-center gap-2 pt-1">
          <input
            type="number"
            min="0"
            max="65535"
            bind:value={newHost}
            placeholder={m.services_ports_extraHostPlaceholder()}
            onkeydown={(e) => e.key === 'Enter' && addExtra()}
            disabled={saving}
            class="w-28 {inputCls}"
          />
          <span class="text-sm text-gray-400 shrink-0">:</span>
          <input
            type="number"
            min="0"
            max="65535"
            bind:value={newContainer}
            placeholder={m.services_ports_extraContainerPlaceholder()}
            onkeydown={(e) => e.key === 'Enter' && addExtra()}
            disabled={saving}
            class="w-28 {inputCls}"
          />
          <DetailButton
            tone="primary"
            onclick={addExtra}
            disabled={saving || newHost == null || newContainer == null}
          >
            {m.common_add()}
          </DetailButton>
        </div>
      {/if}
    </div>

    {#if error}
      <p class="text-xs text-red-500">{error}</p>
    {/if}
  </div>
  {#snippet footer()}
    <button
      type="button"
      onclick={onclose}
      class="text-xs px-3 py-1.5 rounded-sm border border-gray-200 dark:border-lerd-border text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-white/5 transition-colors"
    >{m.common_cancel()}</button>
    <button
      type="button"
      onclick={save}
      disabled={saving}
      class="text-xs px-3 py-1.5 rounded-sm bg-lerd-red hover:bg-lerd-redhov text-white transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
    >{saving ? m.services_ports_applying() : m.common_save()}</button>
  {/snippet}
</Modal>
