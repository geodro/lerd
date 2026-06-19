<script lang="ts">
  import { onMount } from 'svelte';
  import { theme } from '$stores/theme';
  import { loadMonaco, lerdThemeName, type MonacoModule } from '$lib/monaco';
  import type * as Monaco from 'monaco-editor';

  interface Props {
    value: string;
    /** Monaco language id, e.g. 'php', 'ini', 'plaintext'. */
    language?: string;
    onChange?: (next: string) => void;
    readOnly?: boolean;
    /** Extra classes on the editor container. */
    class?: string;
    /** Monaco editor options merged over the defaults. */
    options?: Monaco.editor.IStandaloneEditorConstructionOptions;
    /** Fires once the editor and the monaco module are ready, for callers
        that need to add commands, attach a language client, etc. */
    onReady?: (ctx: { editor: Monaco.editor.IStandaloneCodeEditor; monaco: MonacoModule }) => void;
  }
  let {
    value = $bindable(''),
    language = 'plaintext',
    onChange,
    readOnly = false,
    class: extraClass = '',
    options = {},
    onReady
  }: Props = $props();

  let container: HTMLDivElement | undefined = $state();
  let editor: Monaco.editor.IStandaloneCodeEditor | undefined;
  // Guards external value writes from looping back through onChange.
  let internalUpdate = false;
  let disposed = false;

  onMount(() => {
    let unsubTheme: (() => void) | undefined;
    void (async () => {
      const monaco = await loadMonaco();
      if (disposed || !container) return;

      const ed = monaco.editor.create(container, {
        value,
        language,
        readOnly,
        automaticLayout: true,
        minimap: { enabled: false },
        fontSize: 12,
        fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
        lineNumbersMinChars: 3,
        scrollBeyondLastLine: false,
        wordWrap: 'on',
        renderLineHighlightOnlyWhenFocus: true,
        // Render suggest/hover/parameter-hint widgets with fixed positioning
        // so they escape the `overflow-hidden` card wrapping the editor
        // instead of being clipped to its bounds.
        fixedOverflowWidgets: true,
        padding: { top: 8, bottom: 32 },
        tabSize: 4,
        ...options
      });
      editor = ed;

      ed.onDidChangeModelContent(() => {
        if (internalUpdate) return;
        const next = ed.getValue();
        value = next;
        onChange?.(next);
      });

      // Self-contained theme decision so it stays correct regardless of
      // subscriber ordering against the theme store's own DOM toggle.
      unsubTheme = theme.subscribe((t) => monaco.editor.setTheme(lerdThemeName(t)));

      onReady?.({ editor: ed, monaco });
    })();

    return () => {
      disposed = true;
      unsubTheme?.();
      editor?.dispose();
      editor = undefined;
    };
  });

  // Mirror external value mutations into the editor without re-entering the
  // change listener that would otherwise report them back as user edits.
  $effect(() => {
    const v = value;
    if (!editor) return;
    if (editor.getValue() !== v) {
      internalUpdate = true;
      try {
        editor.setValue(v);
      } finally {
        internalUpdate = false;
      }
    }
  });

  $effect(() => {
    const ro = readOnly;
    editor?.updateOptions({ readOnly: ro });
  });
</script>

<div bind:this={container} class="h-full w-full {extraClass}"></div>
