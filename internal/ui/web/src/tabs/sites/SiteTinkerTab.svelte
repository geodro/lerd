<script lang="ts">
  import { onMount } from 'svelte';
  import { EditorView, keymap, lineNumbers, highlightActiveLine } from '@codemirror/view';
  import { EditorState } from '@codemirror/state';
  import { defaultKeymap, history, historyKeymap } from '@codemirror/commands';
  import {
    autocompletion,
    completionKeymap,
    closeBrackets,
    closeBracketsKeymap,
    completeAnyWord,
    startCompletion,
    acceptCompletion,
    completionStatus,
    type CompletionContext,
    type CompletionResult
  } from '@codemirror/autocomplete';
  import { php } from '@codemirror/lang-php';
  import { linter, lintGutter, type Diagnostic } from '@codemirror/lint';
  import {
    runTinker,
    loadTinkerSymbols,
    lintTinker,
    type TinkerResponse,
    type TinkerSymbols,
    type Site
  } from '$stores/sites';
  import { parseDump, looksLikeDump } from '$lib/dump-parser';
  import DumpView from '$components/DumpView.svelte';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
    branch?: string;
  }
  let { site, branch = '' }: Props = $props();

  const draftKey = $derived(`tinker:${site.domain}${branch ? '@' + branch : ''}:draft`);

  let code = $state('');
  let running = $state(false);
  let result = $state<TinkerResponse | null>(null);
  let editorContainer: HTMLDivElement | undefined = $state();
  let view: EditorView | undefined;
  let symbols: TinkerSymbols = { models: [], classes: [], functions: [] };

  // Backend injects \x1e (record separator) between top-level statement
  // outputs so each `echo`/`dump` becomes its own block in the UI.
  type OutputBlock =
    | { kind: 'tree'; nodes: ReturnType<typeof parseDump>['nodes']; trailing: string; raw: string }
    | { kind: 'error'; type: string; message: string; raw: string }
    | { kind: 'text'; text: string };

  // psysh emits runtime errors on stdout in the form
  //   `Error  Call to a member function get() on int.`
  //   `TypeError  Argument #1 ($x) must be of type int, string given`
  // even though `ok=true` and `exit_code=0`. Detect them so we can render
  // with the same red treatment as backend-level errors.
  const ERROR_RE = /^\s*([A-Z][A-Za-z]+(?:Error|Exception|Throwable))\s{2,}([\s\S]+)$/;

  const stdoutBlocks = $derived.by<OutputBlock[]>(() => {
    if (!result?.stdout) return [];
    return result.stdout
      .split('\x1e')
      .map((chunk) => chunk.replace(/^\n+|\n+$/g, ''))
      .filter((chunk) => chunk.length > 0)
      .map<OutputBlock>((chunk) => {
        const errMatch = chunk.match(ERROR_RE);
        if (errMatch) {
          return {
            kind: 'error',
            type: errMatch[1],
            message: errMatch[2].trim(),
            raw: chunk
          };
        }
        if (looksLikeDump(chunk)) {
          const parsed = parseDump(chunk);
          if (parsed.ok) {
            return { kind: 'tree', nodes: parsed.nodes, trailing: parsed.trailing, raw: chunk };
          }
        }
        return { kind: 'text', text: chunk };
      });
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
    code = '';
    if (view) {
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: '' } });
    }
  }


  $effect(() => {
    const domain = site.domain;
    const b = branch;
    loadTinkerSymbols(domain, b).then((s) => {
      if (site.domain === domain && branch === b) symbols = s;
    });
  });

  onMount(() => {
    if (!editorContainer) return;
    if (typeof localStorage !== 'undefined') {
      const saved = localStorage.getItem(draftKey);
      if (saved) code = saved;
    }
    view = new EditorView({
      parent: editorContainer,
      state: EditorState.create({
        doc: code,
        extensions: [
          lineNumbers(),
          highlightActiveLine(),
          history(),
          closeBrackets(),
          lintGutter(),
          phpLinter,
          php(),
          autocompletion({
            // hintSource:    project + framework + PHP-stdlib + composer fns
            // variableSource: $vars typed earlier in the buffer
            // completeAnyWord: any other word seen in the buffer (fallback)
            override: [hintSource, variableSource, completeAnyWord],
            activateOnTyping: true,
            closeOnBlur: true
          }),
          keymap.of([
            { key: 'Mod-Enter', preventDefault: true, run: () => { run(); return true; } },
            // Tab opens autocomplete; if it's open, accept the selection.
            // Always consumes the key so focus never tabs out of the editor.
            {
              key: 'Tab',
              preventDefault: true,
              run: (v) => {
                if (completionStatus(v.state) === 'active') {
                  acceptCompletion(v);
                  return true;
                }
                startCompletion(v);
                return true;
              }
            },
            ...closeBracketsKeymap,
            ...completionKeymap,
            ...defaultKeymap,
            ...historyKeymap
          ]),
          EditorView.lineWrapping,
          EditorView.updateListener.of((u) => {
            if (u.docChanged) code = u.state.doc.toString();
          }),
          EditorView.theme({
            '&': { height: '100%', fontSize: '12px' },
            '.cm-scroller': { fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace' },
            '.cm-content': { padding: '8px 0' }
          })
        ]
      })
    });
    return () => view?.destroy();
  });

  const placeholder = m.tinker_placeholder();

  // Laravel facades and helpers — built-ins, always offered on Laravel sites.
  const laravelFacades = [
    'Auth', 'Cache', 'Config', 'DB', 'Event', 'Hash', 'Http',
    'Log', 'Mail', 'Notification', 'Queue', 'Redis', 'Request', 'Response',
    'Route', 'Schema', 'Session', 'Storage', 'URL', 'Validator', 'View',
    'Artisan', 'Bus', 'Broadcast', 'Cookie', 'Crypt', 'Date', 'File',
    'Gate', 'Lang', 'Password', 'Process'
  ];
  const laravelHelpers = [
    'collect', 'now', 'today', 'config', 'env', 'cache', 'logger',
    'request', 'session', 'auth', 'response', 'redirect', 'route', 'url',
    'asset', 'optional', 'tap', 'value', 'with', 'data_get', 'data_set',
    'str', 'dd', 'dump', 'class_basename', 'app', 'resolve', 'broadcast', 'event'
  ];

  // Eloquent static methods (called as Model::method()).
  const eloquentStatic = [
    'all', 'find', 'findOrFail', 'findMany', 'first', 'firstWhere',
    'firstOrCreate', 'firstOrNew', 'firstOrFail', 'create', 'make',
    'updateOrCreate', 'where', 'whereIn', 'whereNotIn', 'whereNull',
    'whereNotNull', 'whereBetween', 'whereDate', 'whereHas', 'whereDoesntHave',
    'orderBy', 'latest', 'oldest', 'limit', 'take', 'skip', 'offset',
    'paginate', 'simplePaginate', 'cursor', 'chunk', 'chunkById',
    'count', 'sum', 'avg', 'min', 'max', 'exists', 'doesntExist',
    'pluck', 'value', 'toSql', 'with', 'has', 'select', 'distinct',
    'groupBy', 'having', 'join', 'leftJoin', 'rightJoin', 'union',
    'truncate', 'destroy', 'query', 'newQuery'
  ];

  // Common PHP standard library classes/functions, surfaced regardless of
  // framework. Keeps Tab+autocomplete useful on a fresh project.
  const phpStdClasses = [
    'DateTime', 'DateTimeImmutable', 'DateInterval', 'DateTimeZone',
    'ArrayObject', 'ArrayIterator', 'SplObjectStorage', 'SplFileObject',
    'Closure', 'Generator', 'Iterator', 'IteratorAggregate', 'Countable',
    'Exception', 'Error', 'TypeError', 'ValueError', 'RuntimeException',
    'InvalidArgumentException', 'LogicException', 'PDO', 'PDOStatement',
    'ReflectionClass', 'ReflectionMethod', 'ReflectionFunction',
    'ReflectionProperty', 'ReflectionParameter', 'WeakMap', 'WeakReference',
    'Stringable', 'JsonSerializable'
  ];
  const phpStdFunctions = [
    'json_encode', 'json_decode', 'array_map', 'array_filter', 'array_reduce',
    'array_keys', 'array_values', 'array_merge', 'array_combine', 'array_unique',
    'array_diff', 'array_intersect', 'count', 'in_array', 'sort', 'asort',
    'ksort', 'usort', 'implode', 'explode', 'strpos', 'str_replace',
    'str_contains', 'str_starts_with', 'str_ends_with', 'sprintf', 'printf',
    'preg_match', 'preg_match_all', 'preg_replace', 'preg_split',
    'file_get_contents', 'file_put_contents', 'file_exists', 'is_file',
    'is_dir', 'glob', 'fopen', 'fclose', 'fread', 'fwrite',
    'date', 'time', 'mktime', 'microtime', 'strtotime',
    'var_dump', 'print_r', 'var_export', 'get_class', 'get_object_vars',
    'method_exists', 'property_exists', 'is_array', 'is_string', 'is_numeric',
    'is_object', 'is_null', 'isset', 'empty', 'array_key_exists'
  ];

  // Symfony-flavored hints when we detect a Symfony app. Things people
  // actually type at the REPL when poking at Symfony bundles.
  const symfonyHints = [
    // HTTP Foundation
    'Request', 'Response', 'JsonResponse', 'RedirectResponse', 'StreamedResponse',
    'BinaryFileResponse', 'Cookie', 'HeaderBag', 'ParameterBag', 'Session',
    // Kernel / Container
    'Kernel', 'KernelInterface', 'ContainerInterface', 'ContainerBuilder',
    // Doctrine
    'EntityManager', 'EntityManagerInterface', 'EntityRepository', 'QueryBuilder',
    'Query', 'Connection', 'Schema', 'AbstractMigration',
    // Routing / Annotations / Attributes
    'Route', 'AbstractController', 'Controller', 'Security',
    // DI helpers
    'AutowireServiceLocator', 'TaggedIterator', 'TaggedLocator',
    // Event / Validation / Form
    'Event', 'EventDispatcher', 'EventSubscriberInterface',
    'Validator', 'ValidatorBuilder', 'Constraint', 'NotBlank', 'NotNull',
    'Form', 'FormBuilder', 'FormType', 'AbstractType',
    // Console
    'Command', 'InputInterface', 'OutputInterface', 'SymfonyStyle',
    // Misc
    'Filesystem', 'Finder', 'Process', 'Uuid', 'Ulid'
  ];

  // Eloquent / Builder / Collection instance methods (called as $x->method()).
  const eloquentInstance = [
    'save', 'update', 'delete', 'forceDelete', 'restore', 'fresh', 'refresh',
    'replicate', 'touch', 'push', 'fill', 'forceFill', 'getAttribute',
    'setAttribute', 'getAttributes', 'getOriginal', 'getDirty', 'isDirty',
    'wasChanged', 'toArray', 'toJson', 'load', 'loadMissing', 'loadCount',
    'relationLoaded', 'relationsToArray', 'attributesToArray',
    'where', 'whereIn', 'orWhere', 'orderBy', 'first', 'firstOrFail',
    'get', 'find', 'count', 'sum', 'avg', 'min', 'max', 'pluck', 'paginate',
    'with', 'has', 'whereHas', 'select', 'limit', 'take', 'skip',
    'each', 'map', 'filter', 'reduce', 'reject', 'pipe', 'tap',
    'pluck', 'sort', 'sortBy', 'sortByDesc', 'groupBy', 'keyBy',
    'unique', 'values', 'keys', 'flatten', 'flatMap', 'collapse',
    'merge', 'concat', 'diff', 'intersect', 'only', 'except',
    'contains', 'every', 'some', 'isEmpty', 'isNotEmpty',
    'sum', 'avg', 'min', 'max', 'count', 'reverse', 'shuffle', 'random'
  ];

  function uniq<T>(xs: T[]): T[] {
    return Array.from(new Set(xs));
  }

  // PHP linter — debounced server-side `php -l` check. Each invocation
  // spawns `podman exec lerd-phpXX-fpm php -l`, which is expensive
  // (~50–100 ms of host CPU per call), so we:
  //  1) wait 1.5s after typing stops before linting,
  //  2) memoize on the exact code string so re-runs with no edit reuse
  //     the previous diagnostics,
  //  3) cancel in-flight requests when the doc changes again.
  let lastLintCode = '';
  let lastLintDiags: Diagnostic[] = [];
  let lintAbort: AbortController | null = null;

  const phpLinter = linter(
    async (view) => {
      const code = view.state.doc.toString();
      if (!code.trim()) {
        lastLintCode = '';
        lastLintDiags = [];
        return [];
      }
      if (code === lastLintCode) return lastLintDiags;

      lintAbort?.abort();
      const ctrl = new AbortController();
      lintAbort = ctrl;

      let res;
      try {
        res = await lintTinker(site.domain, code, branch);
      } catch {
        return lastLintDiags;
      }
      if (ctrl.signal.aborted) return lastLintDiags;

      const out: Diagnostic[] = [];
      for (const d of res.diagnostics ?? []) {
        const ln = Math.max(1, Math.min(d.line, view.state.doc.lines));
        const lineObj = view.state.doc.line(ln);
        out.push({
          from: lineObj.from,
          to: lineObj.to,
          severity: d.severity === 'error' ? 'error' : 'warning',
          message: d.message
        });
      }
      lastLintCode = code;
      lastLintDiags = out;
      return out;
    },
    // CodeMirror's linter debounces by `delay`: it waits this many ms
    // after the LAST edit before firing. Combined with the memoize
    // above (return cached if the buffer didn't change since last
    // lint), we get exactly one fire at the end of a typing burst,
    // skipped entirely on no-op refreshes.
    { delay: 600 }
  );

  // Buffer variable source: scans the editor for $varname tokens and
  // suggests them when the user types `$…`. Lets you do `$user = ...;`
  // then `$u` and get `$user` back.
  function variableSource(ctx: CompletionContext): CompletionResult | null {
    const word = ctx.matchBefore(/\$\w*/);
    if (!word || (word.from === word.to && !ctx.explicit)) return null;

    const text = ctx.state.doc.toString();
    const names = new Set<string>();
    const re = /\$([A-Za-z_]\w*)/g;
    let m: RegExpExecArray | null;
    while ((m = re.exec(text)) !== null) names.add(m[1]);

    const cur = word.text.slice(1);
    const opts = Array.from(names)
      .filter((n) => n !== cur)
      .sort()
      .map((name) => ({ label: '$' + name, type: 'variable', detail: 'var' }));
    if (opts.length === 0) return null;
    return { from: word.from, options: opts, validFor: /^\$\w*$/ };
  }

  type Hint = { label: string; type?: string; detail?: string; boost?: number };

  function hintSource(ctx: CompletionContext): CompletionResult | null {
    const before = ctx.state.sliceDoc(Math.max(0, ctx.pos - 200), ctx.pos);

    // After `Model::` — offer Eloquent static methods.
    const staticCall = before.match(/([A-Z][A-Za-z0-9_]*)::([A-Za-z_]\w*)?$/);
    if (staticCall) {
      const word = ctx.matchBefore(/[A-Za-z_]\w*/);
      const from = word ? word.from : ctx.pos;
      return {
        from,
        options: eloquentStatic.map<Hint>((label) => ({ label, type: 'method', detail: 'static' })),
        validFor: /^\w*$/
      };
    }

    // After `->` — offer Eloquent instance / collection methods.
    const arrowCall = before.match(/->([A-Za-z_]\w*)?$/);
    if (arrowCall) {
      const word = ctx.matchBefore(/[A-Za-z_]\w*/);
      const from = word ? word.from : ctx.pos;
      return {
        from,
        options: uniq(eloquentInstance).map<Hint>((label) => ({ label, type: 'method', detail: 'method' })),
        validFor: /^\w*$/
      };
    }

    // Plain identifier context — offer project models/classes for any
    // composer-based project, plus framework hints + PHP standard library
    // + composer-loaded global functions + project-defined functions.
    const word = ctx.matchBefore(/[A-Za-z_]\w*/);
    if (!word || (word.from === word.to && !ctx.explicit)) return null;
    const isSymfony = site.framework === 'symfony';
    const opts: Hint[] = [];
    for (const m of symbols.models) opts.push({ label: m, type: 'class', detail: 'model', boost: 10 });
    for (const c of symbols.classes) {
      if (!symbols.models.includes(c)) opts.push({ label: c, type: 'class', detail: 'class', boost: 5 });
    }
    for (const f of symbols.functions) {
      opts.push({ label: f, type: 'function', detail: 'function', boost: 1 });
    }
    if (site.is_laravel) {
      for (const f of laravelFacades) opts.push({ label: f, type: 'class', detail: 'facade' });
      for (const h of laravelHelpers) opts.push({ label: h, type: 'function', detail: 'helper' });
    }
    if (isSymfony) {
      for (const s of symfonyHints) opts.push({ label: s, type: 'class', detail: 'symfony' });
    }
    for (const c of phpStdClasses) opts.push({ label: c, type: 'class', detail: 'php class' });
    for (const f of phpStdFunctions) opts.push({ label: f, type: 'function', detail: 'php fn' });
    if (opts.length === 0) return null;
    return { from: word.from, options: opts, validFor: /^\w*$/ };
  }
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
      <div class="flex-1 min-h-0 overflow-hidden" bind:this={editorContainer}></div>
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
          <div class="output-row group" data-line={i + 1}>
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
              {:else}
                <pre class="whitespace-pre-wrap">{block.text}</pre>
              {/if}
            </div>
            <button
              onclick={() =>
                copyText(
                  block.kind === 'tree' ? block.raw :
                  block.kind === 'error' ? block.raw : block.text
                )}
              title={m.tinker_copyOutputTitle()}
              class="output-copy opacity-0 group-hover:opacity-100 text-[10px] px-1.5 py-0.5 rounded-sm border border-gray-200 dark:border-lerd-border text-gray-500 hover:text-gray-700 dark:hover:text-gray-200 transition-opacity shrink-0"
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
  /* CodeMirror autocomplete tooltip is portaled to <body>, so component
     scoping doesn't reach it. Use :global() and follow the .dark class on
     <html> that ThemeSwitcher toggles. */
  :global(.cm-tooltip.cm-tooltip-autocomplete) {
    border: 1px solid #e5e7eb;
    background-color: #ffffff;
    color: #111827;
    border-radius: 6px;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.08);
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-size: 12px;
  }
  :global(.cm-tooltip.cm-tooltip-autocomplete > ul > li) {
    padding: 2px 8px;
    color: #111827;
  }
  :global(.cm-tooltip.cm-tooltip-autocomplete > ul > li[aria-selected]) {
    background-color: #ff2d20;
    color: #ffffff;
  }
  :global(.cm-tooltip.cm-tooltip-autocomplete > ul > li) {
    display: flex !important;
    align-items: center;
    gap: 6px;
  }
  :global(.cm-completionIcon) {
    color: #6b7280;
    opacity: 0.85;
    width: 1em;
    flex-shrink: 0;
  }
  :global(.cm-completionLabel) {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  :global(.cm-completionDetail) {
    color: #6b7280;
    font-style: normal;
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    margin-left: auto;
    padding-left: 12px;
    flex-shrink: 0;
  }
  /* Type-specific icon coloring. CodeMirror sets cm-completionIcon-{type} */
  :global(.cm-completionIcon-class) { color: #0ea5e9; }
  :global(.cm-completionIcon-method) { color: #8b5cf6; }
  :global(.cm-completionIcon-function) { color: #10b981; }
  :global(.cm-completionIcon-variable) { color: #f59e0b; }
  :global(.cm-completionIcon-property) { color: #ec4899; }

  :global(html.dark .cm-tooltip.cm-tooltip-autocomplete) {
    border: 1px solid #262626;
    background-color: #161616;
    color: #e5e7eb;
    box-shadow: 0 6px 16px rgba(0, 0, 0, 0.5);
  }
  :global(html.dark .cm-tooltip.cm-tooltip-autocomplete > ul > li) {
    color: #e5e7eb;
  }
  :global(html.dark .cm-tooltip.cm-tooltip-autocomplete > ul > li[aria-selected]) {
    background-color: #ff2d20;
    color: #ffffff;
  }
  :global(html.dark .cm-completionIcon) {
    color: #9ca3af;
  }
  :global(html.dark .cm-completionDetail) {
    color: #9ca3af;
  }
  /* CodeMirror's default gutter is white-on-light. Override for dark
     mode (lerd-card background, muted slate text) so the line numbers
     blend with the rest of the dashboard. */
  :global(html.dark .cm-gutters) {
    background-color: #161616;
    border-right: 1px solid #262626;
    color: #6b7280;
  }
  :global(html.dark .cm-lineNumbers .cm-gutterElement) {
    color: #6b7280;
  }
  :global(html.dark .cm-activeLineGutter) {
    background-color: rgba(255, 255, 255, 0.04);
    color: #d1d5db;
  }

  /* Output panel — visually mirrors the CodeMirror editor on the left:
     bordered box, monospace, line-number gutter that the user can't
     mouse-select or copy. Numbers come from `data-line` via `::before`,
     so they're CSS-generated content (excluded from text selection in
     all modern browsers). */
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
  .tinker-output :global(.output-row::before) {
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
  :global(html.dark) .tinker-output :global(.output-row::before) {
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

  :global(html.dark .cm-completionIcon-class) { color: #38bdf8; }
  :global(html.dark .cm-completionIcon-method) { color: #a78bfa; }
  :global(html.dark .cm-completionIcon-function) { color: #34d399; }
  :global(html.dark .cm-completionIcon-variable) { color: #fbbf24; }
  :global(html.dark .cm-completionIcon-property) { color: #f472b6; }
</style>
