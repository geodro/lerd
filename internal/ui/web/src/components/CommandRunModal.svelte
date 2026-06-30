<script lang="ts">
  import { currentRun, closeRun, executeCommand, runToast, lastRunFor, type RunLine } from '$stores/commands';
  import { m } from '../paraglide/messages.js';
  import { ansiToHtml } from '$lib/ansi';

  function relativeTime(ts: number): string {
    const diffMs = Date.now() - ts;
    const s = Math.floor(diffMs / 1000);
    if (s < 60) return s + 's ago';
    const m = Math.floor(s / 60);
    if (m < 60) return m + 'm ago';
    const h = Math.floor(m / 60);
    if (h < 24) return h + 'h ago';
    return Math.floor(h / 24) + 'd ago';
  }

  function viewPrevious() {
    if ($currentRun.kind !== 'confirm') return;
    const prev = lastRunFor($currentRun.domain, $currentRun.cmd.name);
    if (!prev) return;
    currentRun.set({
      kind: 'done',
      domain: prev.domain,
      cmd: $currentRun.cmd,
      lines: prev.lines,
      exit: prev.exit,
      durationMs: prev.durationMs,
      url: prev.url
    });
  }

  // Render each line with a color hint based on its source stream. stderr
  // gets a soft red prefix, meta (lerd's own [error] / [aborted] markers)
  // gets a yellow prefix. The ANSI renderer handles the inline escapes
  // alongside any colors the command itself emitted.
  function renderLines(lines: RunLine[]): string {
    return lines
      .map((l) => {
        if (l.stream === 'stderr') return '\x1b[31m' + l.text + '\x1b[0m';
        if (l.stream === 'meta') return '\x1b[33m' + l.text + '\x1b[0m';
        return l.text;
      })
      .join('\n');
  }

  let copied = $state(false);
  let toast: string | null = $state(null);

  $effect(() => {
    const unsub = runToast.subscribe((v) => (toast = v));
    return unsub;
  });

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== 'Escape') return;
    if ($currentRun.kind === 'idle') return;
    e.preventDefault();
    closeRun();
  }

  $effect(() => {
    window.addEventListener('keydown', onKeydown);
    return () => window.removeEventListener('keydown', onKeydown);
  });

  function copyUrl(url: string) {
    void navigator.clipboard.writeText(url);
    copied = true;
    setTimeout(() => (copied = false), 1400);
  }

  const cmd = $derived.by(() => {
    const s = $currentRun;
    if (s.kind === 'idle') return null;
    return s.cmd;
  });
</script>

{#if $currentRun.kind === 'confirm' && cmd}
  <div class="fixed inset-0 z-50 flex items-center justify-center">
    <button class="absolute inset-0 bg-black/50" aria-label={m.common_cancel()} onclick={closeRun}></button>
    <div class="relative bg-white dark:bg-lerd-card border border-gray-200 dark:border-lerd-border rounded-xl shadow-2xl w-full max-w-md mx-4 p-5">
      <div class="flex items-start gap-3">
        <span class="shrink-0 w-9 h-9 rounded-full bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300 flex items-center justify-center">
          <svg class="w-5 h-5" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01M5 19h14a2 2 0 001.74-3l-7-12a2 2 0 00-3.48 0l-7 12A2 2 0 005 19z" />
          </svg>
        </span>
        <div class="flex-1 min-w-0">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{m.cmdrun_runTitle({ label: cmd.label || cmd.name })}</h3>
          {#if $currentRun.kind === 'confirm'}
            <p class="text-[11px] text-gray-500 dark:text-gray-400 mt-0.5">on {$currentRun.domain}</p>
          {/if}
          {#if cmd.description}
            <p class="text-xs text-gray-600 dark:text-gray-400 mt-2">{cmd.description}</p>
          {/if}
          <pre class="mt-3 px-3 py-2 rounded bg-gray-100 dark:bg-black/40 text-[11px] font-mono text-gray-800 dark:text-gray-200 overflow-x-auto">$ {cmd.command}</pre>
        </div>
      </div>
      {#if $currentRun.kind === 'confirm'}
        {@const prev = lastRunFor($currentRun.domain, cmd.name)}
        {#if prev}
          <div class="mt-3 flex items-center gap-2 text-[11px] text-gray-500 dark:text-gray-400 pl-12">
            <span>{m.cmdrun_lastRun({ exit: prev.exit, ms: prev.durationMs, time: relativeTime(prev.finishedAt) })}</span>
            <button onclick={viewPrevious} class="text-lerd-red hover:underline">{m.cmdrun_viewOutput()}</button>
          </div>
        {/if}
      {/if}
      <div class="mt-5 flex items-center justify-end gap-2">
        <button onclick={closeRun} class="px-3 py-1.5 rounded-md text-xs font-medium text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-white/5">{m.common_cancel()}</button>
        <button
          onclick={() => $currentRun.kind === 'confirm' && executeCommand($currentRun.domain, cmd, '', true)}
          class="px-3 py-1.5 rounded-md text-xs font-medium bg-lerd-red hover:bg-lerd-redhov text-white"
        >{m.cmdrun_runAnyway()}</button>
      </div>
    </div>
  </div>
{/if}

{#if ($currentRun.kind === 'running' || $currentRun.kind === 'done') && cmd}
  <div class="fixed inset-0 z-50 flex items-center justify-center">
    <button class="absolute inset-0 bg-black/50" aria-label={m.common_close()} onclick={closeRun}></button>
    <div class="relative bg-white dark:bg-lerd-card border border-gray-200 dark:border-lerd-border rounded-xl shadow-2xl w-full max-w-2xl mx-4">
      <header class="flex items-center justify-between px-5 py-4 border-b border-gray-100 dark:border-lerd-border">
        <div class="min-w-0">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white truncate">{cmd.label || cmd.name}</h3>
          <p class="text-[11px] font-mono text-gray-500 dark:text-gray-400 truncate mt-0.5">{$currentRun.domain} · $ {cmd.command}</p>
        </div>
        <button onclick={closeRun} aria-label={m.common_close()} class="shrink-0 text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 ml-3">
          <svg class="w-5 h-5" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
          </svg>
        </button>
      </header>

      <div class="px-5 py-4">
        <div class="flex items-center gap-2 mb-3">
          {#if $currentRun.kind === 'running'}
            <span class="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[11px] font-semibold bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300">
              <svg class="w-3 h-3 animate-spin" fill="none" viewBox="0 0 24 24"><circle class="opacity-30" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" /><path class="opacity-90" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z" /></svg>
              running
            </span>
          {:else if $currentRun.kind === 'done' && $currentRun.exit === 0}
            <span class="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[11px] font-semibold bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300">
              <svg class="w-3 h-3" fill="none" stroke="currentColor" stroke-width="3" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" /></svg>
              {m.cmdrun_exitCode({ code: 0 })}
            </span>
          {:else if $currentRun.kind === 'done'}
            <span class="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[11px] font-semibold bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300">{m.cmdrun_exitCode({ code: $currentRun.exit })}</span>
          {/if}
          {#if $currentRun.kind === 'done'}
            <span class="text-[11px] text-gray-500 dark:text-gray-400">{$currentRun.durationMs}ms</span>
          {/if}
        </div>

        {#if $currentRun.kind === 'done' && cmd.output === 'url' && $currentRun.url}
          <div class="mb-3 rounded-md border border-gray-200 dark:border-lerd-border bg-gray-50 dark:bg-black/30 p-3">
            <p class="text-[11px] text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-2">{m.cmdrun_oneTimeUrl()}</p>
            <div class="flex items-center gap-2">
              <code class="flex-1 min-w-0 text-xs font-mono text-gray-800 dark:text-gray-100 truncate">{$currentRun.url}</code>
              <button onclick={() => $currentRun.kind === 'done' && copyUrl($currentRun.url!)} class="shrink-0 px-2 py-1 rounded text-[11px] font-medium bg-gray-200 hover:bg-gray-300 dark:bg-white/10 dark:hover:bg-white/20 text-gray-800 dark:text-gray-100">
                {copied ? 'Copied' : 'Copy'}
              </button>
              <a href={$currentRun.url} target="_blank" rel="noopener" class="shrink-0 px-2 py-1 rounded text-[11px] font-medium bg-lerd-red hover:bg-lerd-redhov text-white">{m.common_open()}</a>
            </div>
          </div>
        {/if}

        {#if ($currentRun.kind === 'running' || $currentRun.kind === 'done') && $currentRun.lines.length > 0}
          <pre class="max-h-[50vh] overflow-auto no-scrollbar rounded-md bg-black/90 text-gray-100 text-[11px] font-mono p-3 leading-relaxed whitespace-pre-wrap">{@html ansiToHtml(renderLines($currentRun.lines))}</pre>
        {:else}
          <pre class="max-h-[50vh] overflow-auto no-scrollbar rounded-md bg-black/90 text-gray-100 text-[11px] font-mono p-3 leading-relaxed whitespace-pre-wrap">{$currentRun.kind === 'running' ? m.cmdrun_waitingOutput() : m.cmdrun_noOutput()}</pre>
        {/if}
      </div>

      <footer class="px-5 py-3 border-t border-gray-100 dark:border-lerd-border flex items-center justify-end">
        <button onclick={closeRun} class="px-3 py-1.5 rounded-md text-xs font-medium text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-white/5">
          {$currentRun.kind === 'running' ? m.common_cancel() : m.common_close()}
        </button>
      </footer>
    </div>
  </div>
{/if}

{#if toast}
  <div class="fixed bottom-4 right-4 z-50 px-3 py-2 rounded-md shadow-lg bg-gray-900 text-white text-xs font-medium">
    {toast}
  </div>
{/if}
