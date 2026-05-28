<script lang="ts">
  import {
    EditorView,
    keymap,
    lineNumbers,
    highlightActiveLine,
    Decoration,
    ViewPlugin
  } from '@codemirror/view';
  import type { DecorationSet, ViewUpdate } from '@codemirror/view';
  import { RangeSetBuilder } from '@codemirror/state';
  import { defaultKeymap, history, historyKeymap } from '@codemirror/commands';
  import CodeEditor from './CodeEditor.svelte';

  interface Props {
    value: string;
    readOnly?: boolean;
    onChange?: (next: string) => void;
  }
  let { value = $bindable(''), readOnly = false, onChange }: Props = $props();

  // Dotenv-flavoured highlighter. Five token classes (key, op, value, string,
  // comment) decorated via regex over visible lines. Light enough to live
  // inside the component without pulling @codemirror/language StreamLanguage.
  const KV_RE = /^(\s*)([A-Za-z_][A-Za-z0-9_]*)(\s*=\s*)(.*)$/;
  const STRING_RE = /^(?:"[^"]*"|'[^']*')\s*$/;

  const envHighlighter = ViewPlugin.fromClass(
    class {
      decorations: DecorationSet;
      constructor(v: EditorView) {
        this.decorations = this.build(v);
      }
      update(u: ViewUpdate) {
        if (u.docChanged || u.viewportChanged) this.decorations = this.build(u.view);
      }
      build(v: EditorView): DecorationSet {
        const b = new RangeSetBuilder<Decoration>();
        for (const { from, to } of v.visibleRanges) {
          let pos = from;
          while (pos <= to) {
            const line = v.state.doc.lineAt(pos);
            const text = line.text;
            if (/^\s*#/.test(text)) {
              b.add(line.from, line.to, Decoration.mark({ class: 'cm-env-comment' }));
            } else {
              const m = KV_RE.exec(text);
              if (m) {
                const [, lead, key, eq, val] = m;
                const keyStart = line.from + lead.length;
                const keyEnd = keyStart + key.length;
                const opStart = keyEnd;
                const opEnd = opStart + eq.length;
                b.add(keyStart, keyEnd, Decoration.mark({ class: 'cm-env-key' }));
                b.add(opStart, opEnd, Decoration.mark({ class: 'cm-env-op' }));
                if (val.length > 0) {
                  const valStart = opEnd;
                  const valEnd = line.to;
                  const cls = STRING_RE.test(val) ? 'cm-env-string' : 'cm-env-value';
                  b.add(valStart, valEnd, Decoration.mark({ class: cls }));
                }
              }
            }
            pos = line.to + 1;
            if (line.to >= v.state.doc.length) break;
          }
        }
        return b.finish();
      }
    },
    { decorations: (v) => v.decorations }
  );

  const envExtensions = [
    lineNumbers(),
    highlightActiveLine(),
    history(),
    envHighlighter,
    keymap.of([...defaultKeymap, ...historyKeymap]),
    EditorView.lineWrapping
  ];
</script>

<div class="env-editor h-full w-full">
  <CodeEditor bind:value {readOnly} {onChange} extensions={envExtensions} />
</div>

<style>
  .env-editor :global(.cm-env-key) {
    color: #1d4ed8;
    font-weight: 500;
  }
  :global(.dark) .env-editor :global(.cm-env-key) {
    color: #93c5fd;
  }
  .env-editor :global(.cm-env-op) {
    color: #9ca3af;
  }
  .env-editor :global(.cm-env-value) {
    color: #374151;
  }
  :global(.dark) .env-editor :global(.cm-env-value) {
    color: #e5e7eb;
  }
  .env-editor :global(.cm-env-string) {
    color: #047857;
  }
  :global(.dark) .env-editor :global(.cm-env-string) {
    color: #6ee7b7;
  }
  .env-editor :global(.cm-env-comment) {
    color: #9ca3af;
    font-style: italic;
  }
</style>
