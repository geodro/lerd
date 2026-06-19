<script lang="ts">
  import { runTinker, type TinkerResponse, type Site } from '$stores/sites';
  import { parseDump, looksLikeDump } from '$lib/dump-parser';
  import { parseBlock } from '$lib/tinker';
  import DumpView from '$components/DumpView.svelte';
  import MonacoEditor from '$components/MonacoEditor.svelte';
  import Icon from '$components/Icon.svelte';
  import { attachPhpLsp, type PhpLspHandle } from '$lib/lsp';
  import type { MonacoModule } from '$lib/monaco';
  import type * as Monaco from 'monaco-editor';
  import { onDestroy } from 'svelte';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
    branch?: string;
  }
  let { site, branch = '' }: Props = $props();

  const draftKey = $derived(`tinker:${site.domain}${branch ? '@' + branch : ''}:draft`);

  // Seed the editor with the saved draft once at construction. We
  // deliberately read site/branch as initial values (not reactive) here
  // because the draft only needs to be loaded once; the persisting
  // $effect below keeps localStorage in sync on every edit thereafter.
  function loadInitialDraft(): string {
    if (typeof localStorage === 'undefined') return '';
    const key = `tinker:${site.domain}${branch ? '@' + branch : ''}:draft`;
    return localStorage.getItem(key) ?? '';
  }
  let code = $state(loadInitialDraft());
  let running = $state(false);
  let result = $state<TinkerResponse | null>(null);

  // Backend frames each top-level statement's output as `\x1e<line>\x1f<out>`:
  // the record separator splits blocks, and `line` is the editor line that
  // produced the block (rendered as a "Line N" badge).
  type OutputBlock =
    | { kind: 'tree'; nodes: ReturnType<typeof parseDump>['nodes']; trailing: string; raw: string; line?: number }
    | { kind: 'error'; type: string; message: string; raw: string; line?: number }
    | { kind: 'query'; sql: string; line?: number }
    | { kind: 'text'; text: string; line?: number };

  // psysh emits runtime errors on stdout in the form
  //   `Error  Call to a member function get() on int.`
  //   `TypeError  Argument #1 ($x) must be of type int, string given`
  // even though `ok=true` and `exit_code=0`. Detect them so we can render
  // with the same red treatment as backend-level errors.
  const ERROR_RE = /^\s*([A-Z][A-Za-z]+(?:Error|Exception|Throwable))\s{2,}([\s\S]+)$/;

  const stdoutBlocks = $derived.by<OutputBlock[]>(() => {
    if (!result?.stdout) return [];
    const blocks: OutputBlock[] = [];
    for (const rawChunk of result.stdout.split('\x1e')) {
      // Peel the `<line>\x1f` marker before trimming so a block with only the
      // marker (a no-output statement) drops out as empty.
      const { line, kind, body } = parseBlock(rawChunk);
      const chunk = body.replace(/^\n+|\n+$/g, '');
      if (chunk.length === 0) continue;
      if (kind === 'query') {
        blocks.push({ kind: 'query', sql: chunk, line });
        continue;
      }
      const errMatch = chunk.match(ERROR_RE);
      if (errMatch) {
        blocks.push({ kind: 'error', type: errMatch[1], message: errMatch[2].trim(), raw: chunk, line });
        continue;
      }
      if (looksLikeDump(chunk)) {
        const parsed = parseDump(chunk);
        if (parsed.ok) {
          blocks.push({ kind: 'tree', nodes: parsed.nodes, trailing: parsed.trailing, raw: chunk, line });
          continue;
        }
      }
      blocks.push({ kind: 'text', text: chunk, line });
    }
    return blocks;
  });

  async function copyText(text: string) {
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      // Fall back to a hidden textarea for non-secure contexts.
      const ta = document.createElement('textarea');
      ta.value = text;
      ta.style.position = 'fixed';
      ta.style.left = '-9999px';
      document.body.appendChild(ta);
      ta.select();
      try { document.execCommand('copy'); } catch (_) { /* ignore */ }
      document.body.removeChild(ta);
    }
  }

  $effect(() => {
    if (typeof localStorage !== 'undefined') {
      localStorage.setItem(draftKey, code);
    }
  });

  async function run() {
    if (running || !code.trim()) return;
    running = true;
    result = null;
    try {
      result = await runTinker(site.domain, code, branch);
    } finally {
      running = false;
    }
  }

  function clearAll() {
    result = null;
    // MonacoEditor's $effect mirrors external value writes into the editor,
    // so assigning '' here clears the doc without us needing an editor ref.
    code = '';
  }

  // LSP status drives the small indicator next to the mode badge. phpantom
  // backs completion, diagnostics, and hover from the real project.
  let lspStatus = $state<'connecting' | 'ready' | 'unavailable'>('connecting');
  let lsp: PhpLspHandle | null = null;

  // Mod-Enter runs the buffer; the LSP attaches to the live editor. The
  // closure reads the current `code` state on each invocation.
  function onEditorReady({ editor, monaco }: { editor: Monaco.editor.IStandaloneCodeEditor; monaco: MonacoModule }) {
    // Intercept at the keydown level rather than via addCommand: Ctrl/Cmd+Enter
    // must run even while the suggestion widget is open (which otherwise
    // captures Enter to accept the highlighted completion).
    editor.onKeyDown((e) => {
      if ((e.ctrlKey || e.metaKey) && e.keyCode === monaco.KeyCode.Enter) {
        e.preventDefault();
        e.stopPropagation();
        void run();
      }
    });

    lsp?.dispose();
    lsp = attachPhpLsp({
      monaco,
      editor,
      domain: site.domain,
      branch,
      onStatus: (s) => { lspStatus = s; }
    });
  }

  onDestroy(() => lsp?.dispose());

  const placeholder = m.tinker_placeholder();
</script>

<div class="flex-1 flex flex-col min-h-0 overflow-hidden pt-4 px-3 sm:px-5 pb-3 sm:pb-5 gap-3">
  <div class="flex items-center justify-between">
    <div class="flex items-center gap-2">
      <span
        class="text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded-sm border border-gray-200 dark:border-lerd-border text-gray-500 dark:text-gray-400"
        title={result?.mode === 'tinker' ? m.tinker_mode_tinkerTitle() : m.tinker_mode_phpTitle()}
      >
        {result?.mode ?? (site.is_laravel ? 'tinker' : 'php')}
      </span>
      {#if result}
        <span class="text-[10px] text-gray-400">{result.duration_ms} ms</span>
      {/if}
      {#if lspStatus !== 'ready'}
        <span
          class="text-[10px] {lspStatus === 'unavailable' ? 'text-amber-500' : 'text-gray-400'}"
          title={lspStatus === 'unavailable' ? m.tinker_lspUnavailable() : m.tinker_lspConnecting()}
        >{lspStatus === 'unavailable' ? m.tinker_lspUnavailable() : m.tinker_lspConnecting()}</span>
      {/if}
    </div>
    <div class="flex items-center gap-2">
      <button
        onclick={clearAll}
        disabled={!code && !result}
        class="text-xs px-2 py-1 rounded-sm border border-gray-200 dark:border-lerd-border text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-white/5 disabled:opacity-40"
        title={m.tinker_clearTitle()}
      >{m.common_clear()}</button>
      <button
        onclick={run}
        disabled={running || !code.trim()}
        class="text-xs px-3 py-1 rounded-sm bg-lerd-red hover:bg-lerd-redhov text-white disabled:opacity-40 transition-colors"
        title={m.tinker_runTitle()}
      >
        {running ? m.tinker_running() : m.tinker_run()}
      </button>
    </div>
  </div>

  <div class="flex-1 flex flex-col md:flex-row min-h-0 gap-3">
    <div
      class="group flex-1 min-h-[160px] md:min-h-0 md:basis-1/2 flex flex-col rounded-lg border border-gray-200 dark:border-lerd-border overflow-hidden bg-gray-50 dark:bg-black/40 relative"
    >
      <div class="flex-1 min-h-0 overflow-hidden">
        <MonacoEditor bind:value={code} language="php" onReady={onEditorReady} />
      </div>
      {#if code.trim()}
        <button
          onclick={() => copyText(code)}
          title={m.tinker_copyEditorTitle()}
          class="absolute top-2 right-2 z-10 opacity-0 group-hover:opacity-100 text-[10px] px-1.5 py-0.5 rounded-sm border border-gray-200 dark:border-lerd-border bg-white/90 dark:bg-lerd-card/90 text-gray-500 hover:text-gray-700 dark:hover:text-gray-200 transition-opacity"
        >{m.common_copy()}</button>
      {/if}
    </div>

    <div
      class="flex-1 min-h-[120px] md:min-h-0 md:basis-1/2 flex flex-col overflow-y-auto rounded-lg border border-gray-200 dark:border-lerd-border bg-gray-50 dark:bg-black/40 tinker-output py-2"
    >
      {#if !result && running}
        <p class="text-xs text-gray-400">{m.tinker_running()}</p>
      {:else if !result}
        <p class="text-[11px] text-gray-400 dark:text-gray-500 font-mono whitespace-pre-line">{placeholder}</p>
      {:else}
        {#if result.error}
          <div class="output-row" data-line="!">
            <div class="output-content text-red-700 dark:text-red-300">
              <pre class="whitespace-pre-wrap">{result.error}</pre>
            </div>
          </div>
        {/if}
        {#each stdoutBlocks as block, i (i)}
          <div class="output-row group" data-line="">
            <div class="output-content">
              {#if block.kind === 'tree'}
                {#each block.nodes as node, j (j)}
                  <div class="mb-1 last:mb-0"><DumpView {node} /></div>
                {/each}
                {#if block.trailing.trim()}
                  <pre class="whitespace-pre-wrap text-gray-700 dark:text-gray-300">{block.trailing}</pre>
                {/if}
              {:else if block.kind === 'error'}
                <div class="flex items-start gap-2">
                  <span class="text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded-sm bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-300 shrink-0">{block.type}</span>
                  <pre class="whitespace-pre-wrap text-red-700 dark:text-red-300">{block.message}</pre>
                </div>
              {:else if block.kind === 'query'}
                <div class="flex items-start gap-2 rounded-md border-l-2 border-sky-400 dark:border-sky-500 border-y border-r border-y-sky-200/60 border-r-sky-200/60 dark:border-y-sky-800/40 dark:border-r-sky-800/40 bg-sky-50/80 dark:bg-sky-950/30 px-2 py-1">
                  <Icon name="database" class="w-3.5 h-3.5 mt-[2px] text-sky-500 dark:text-sky-400 shrink-0" />
                  <pre class="whitespace-pre-wrap text-[11px] leading-relaxed text-sky-800 dark:text-sky-300">{block.sql}</pre>
                </div>
              {:else}
                <pre class="whitespace-pre-wrap">{block.text}</pre>
              {/if}
            </div>
            {#if block.line !== undefined && block.kind !== 'query'}
              <span
                class="output-line shrink-0 select-none text-[10px] text-gray-400 dark:text-gray-500"
                title={m.tinker_lineTitle({ n: block.line })}
              >{m.tinker_lineLabel({ n: block.line })}</span>
            {/if}
            <button
              onclick={() =>
                copyText(
                  block.kind === 'tree' ? block.raw :
                  block.kind === 'error' ? block.raw :
                  block.kind === 'query' ? block.sql : block.text
                )}
              title={m.tinker_copyOutputTitle()}
              class="output-copy opacity-0 group-hover:opacity-100 text-[10px] px-1.5 py-0.5 rounded-sm border border-gray-200 dark:border-lerd-border text-gray-500 hover:text-gray-700 dark:hover:text-gray-200 transition-opacity shrink-0 {block.kind === 'query' ? 'output-copy--abs' : ''}"
            >{m.common_copy()}</button>
          </div>
        {/each}
        {#if result.stderr}
          <div class="output-row" data-line="e">
            <div class="output-content text-amber-700 dark:text-amber-300">
              <pre class="whitespace-pre-wrap">{result.stderr}</pre>
            </div>
          </div>
        {/if}
        {#if stdoutBlocks.length === 0 && !result.stderr && !result.error}
          <div class="output-row" data-line="·">
            <div class="output-content text-gray-400">{m.tinker_noOutput()}</div>
          </div>
        {/if}
      {/if}
    </div>
  </div>
</div>

<style>
  /* Output panel, visually mirrors the editor on the left: bordered box,
     monospace, line-number gutter that the user can't mouse-select or copy.
     Numbers come from `data-line` via `::before`, so they're CSS-generated
     content (excluded from text selection in all modern browsers). */
  .tinker-output {
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-size: 12px;
    line-height: 1.5;
  }
  .tinker-output :global(.output-row) {
    display: flex;
    align-items: flex-start;
    padding: 2px 8px 2px 0;
    position: relative;
  }
  /* Gutter only renders for rows that carry a marker (error/stderr/no-output).
     Result and query rows use data-line="", so it collapses, their info is on
     the right ("Line N" badge), leaving no dead column on the left. */
  .tinker-output :global(.output-row:not([data-line=''])::before) {
    content: attr(data-line);
    flex-shrink: 0;
    width: 32px;
    padding-right: 8px;
    text-align: right;
    color: #9ca3af;
    font-size: 11px;
    user-select: none;
    -webkit-user-select: none;
    pointer-events: none;
  }
  :global(html.dark) .tinker-output :global(.output-row:not([data-line=''])::before) {
    color: #4b5563;
  }
  .tinker-output :global(.output-content) {
    flex: 1;
    min-width: 0;
    padding-left: 8px;
  }
  .tinker-output :global(.output-copy) {
    margin-left: 8px;
  }
  .tinker-output :global(.output-line) {
    margin-left: 8px;
    padding-top: 1px;
    white-space: nowrap;
  }
  /* Query rows keep the result gutter/left padding but float the copy button
     so the card can span the full width to the right edge. */
  .tinker-output :global(.output-copy--abs) {
    position: absolute;
    top: 4px;
    right: 6px;
    margin-left: 0;
  }
</style>
