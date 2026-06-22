<script lang="ts">
  import { onMount } from 'svelte';
  import { loadDnsUpstream, saveDnsUpstream, isValidUpstream } from '$stores/dns';
  import { m } from '../../paraglide/messages.js';

  let original = $state<string>('');
  let text = $state<string>('');
  let detected = $state<string[]>([]);
  let loading = $state(true);
  let loaded = $state(false);
  let saving = $state(false);
  let error = $state<string>('');
  let saved = $state(false);

  function parse(value: string): string[] {
    return value
      .split('\n')
      .map((l) => l.trim())
      .filter((l) => l.length > 0);
  }

  // Compare parsed lists so cosmetic whitespace/blank-line edits don't count as
  // dirty and match exactly what save() would persist.
  const entries = $derived(parse(text));
  const invalid = $derived(entries.filter((e) => !isValidUpstream(e)));
  const dirty = $derived(JSON.stringify(entries) !== JSON.stringify(parse(original)));
  const canSave = $derived(loaded && dirty && invalid.length === 0 && !saving);

  async function load() {
    loading = true;
    error = '';
    try {
      const res = await loadDnsUpstream();
      original = res.upstream.join('\n');
      text = original;
      detected = res.detected;
      loaded = true;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
    loading = false;
  }

  onMount(load);

  // Any edit invalidates the last save confirmation.
  function onInput() {
    saved = false;
  }

  function revert() {
    text = original;
    saved = false;
  }

  // Reset clears every entered server (auto-detection resumes once saved).
  function reset() {
    text = '';
    saved = false;
  }

  async function save() {
    if (!canSave) return;
    saving = true;
    error = '';
    saved = false;
    const r = await saveDnsUpstream(entries);
    if (r.ok) {
      original = (r.upstream ?? entries).join('\n');
      text = original;
      saved = true;
    } else {
      error = r.error || m.system_dns_upstream_applyFailed();
    }
    saving = false;
  }
</script>

<div class="flex-1 overflow-y-auto px-3 py-3 space-y-3">
  <div>
    <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{m.system_dns_upstream_title()}</h3>
    <p class="text-xs text-gray-500 dark:text-gray-400 mt-1">{m.system_dns_upstream_desc()}</p>
  </div>

  {#if loading}
    <p class="text-xs text-gray-400">{m.common_loading()}</p>
  {:else if !loaded}
    <p class="text-xs text-red-500 dark:text-red-400">{error}</p>
    <div class="flex justify-end">
      <button
        type="button"
        onclick={load}
        class="px-3 py-1.5 rounded-lg text-sm font-medium bg-gray-100 hover:bg-gray-200 dark:bg-white/5 dark:hover:bg-white/10 text-gray-700 dark:text-gray-300 transition-colors"
      >{m.common_retry()}</button>
    </div>
  {:else}
    <textarea
      bind:value={text}
      oninput={onInput}
      spellcheck="false"
      autocomplete="off"
      placeholder={m.system_dns_upstream_placeholder()}
      class="w-full h-32 font-mono text-xs rounded-sm border bg-gray-50 dark:bg-black/40 text-gray-800 dark:text-gray-200 px-2.5 py-2 focus:outline-none focus:ring-1 {invalid.length > 0
        ? 'border-red-400 dark:border-red-500 focus:ring-red-500/40'
        : 'border-gray-200 dark:border-lerd-border focus:ring-lerd-red/40'}"
    ></textarea>

    <p class="text-xs text-gray-400">
      {#if detected.length > 0}
        {m.system_dns_upstream_detected({ servers: detected.join(', ') })}
      {:else}
        {m.system_dns_upstream_detectedNone()}
      {/if}
    </p>

    {#if invalid.length > 0}
      <p class="text-xs text-red-500 dark:text-red-400">{m.system_dns_upstream_invalid({ entries: invalid.join(', ') })}</p>
    {:else if error}
      <p class="text-xs text-red-500 dark:text-red-400">{error}</p>
    {:else if saved}
      <p class="text-xs text-emerald-600 dark:text-emerald-400">{m.system_dns_upstream_saved()}</p>
    {/if}

    <div class="flex items-center justify-end gap-2 pt-1">
      {#if entries.length > 0}
        <button
          type="button"
          onclick={reset}
          disabled={saving}
          class="px-3 py-1.5 rounded-lg text-sm font-medium bg-gray-100 hover:bg-gray-200 dark:bg-white/5 dark:hover:bg-white/10 text-gray-700 dark:text-gray-300 disabled:opacity-50 transition-colors"
        >{m.system_dns_upstream_reset()}</button>
      {/if}
      {#if dirty}
        <button
          type="button"
          onclick={revert}
          disabled={saving}
          class="px-3 py-1.5 rounded-lg text-sm font-medium bg-gray-100 hover:bg-gray-200 dark:bg-white/5 dark:hover:bg-white/10 text-gray-700 dark:text-gray-300 disabled:opacity-50 transition-colors"
        >{m.common_cancel()}</button>
      {/if}
      <button
        type="button"
        onclick={save}
        disabled={!canSave}
        class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium bg-lerd-red hover:bg-lerd-redhov text-white disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
      >
        {#if saving}
          <svg class="w-3.5 h-3.5 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z" />
          </svg>
        {/if}
        {m.common_save()}
      </button>
    </div>
  {/if}
</div>
