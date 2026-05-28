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

  // Tuning files cover both mysql / mariadb my.cnf (ini-style with
  // [section] headers, `key = value`, `#` and `;` comments) and redis
  // .conf (`directive arg arg`, `#` comments, no sections). Highlight
  // both shapes by detecting: comments first, then [section] headers,
  // then key=value (mysql), then otherwise treat the first whitespace-
  // separated token as a directive name (redis).
  const COMMENT_RE = /^\s*[#;].*$/;
  const SECTION_RE = /^\s*\[[^\]]+\]\s*$/;
  const KV_RE = /^(\s*)([A-Za-z_][A-Za-z0-9_.-]*)(\s*=\s*)(.*)$/;
  const DIRECTIVE_RE = /^(\s*)([A-Za-z_][A-Za-z0-9_-]*)\b/;

  const tuningHighlighter = ViewPlugin.fromClass(
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
            if (COMMENT_RE.test(text)) {
              b.add(line.from, line.to, Decoration.mark({ class: 'cm-tuning-comment' }));
            } else if (SECTION_RE.test(text)) {
              b.add(line.from, line.to, Decoration.mark({ class: 'cm-tuning-section' }));
            } else {
              const kv = KV_RE.exec(text);
              if (kv) {
                const [, lead, key, eq, val] = kv;
                const keyStart = line.from + lead.length;
                const keyEnd = keyStart + key.length;
                const opEnd = keyEnd + eq.length;
                b.add(keyStart, keyEnd, Decoration.mark({ class: 'cm-tuning-key' }));
                b.add(keyEnd, opEnd, Decoration.mark({ class: 'cm-tuning-op' }));
                if (val.length > 0) {
                  b.add(opEnd, line.to, Decoration.mark({ class: 'cm-tuning-value' }));
                }
              } else {
                const dm = DIRECTIVE_RE.exec(text);
                if (dm) {
                  const [, lead, dir] = dm;
                  const dirStart = line.from + lead.length;
                  b.add(dirStart, dirStart + dir.length, Decoration.mark({ class: 'cm-tuning-key' }));
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

  const tuningExtensions = [
    lineNumbers(),
    highlightActiveLine(),
    history(),
    tuningHighlighter,
    keymap.of([...defaultKeymap, ...historyKeymap]),
    EditorView.lineWrapping
  ];
</script>

<div class="tuning-editor h-full w-full">
  <CodeEditor bind:value {readOnly} {onChange} extensions={tuningExtensions} />
</div>

<style>
  .tuning-editor :global(.cm-tuning-key) {
    color: #1d4ed8;
    font-weight: 500;
  }
  :global(.dark) .tuning-editor :global(.cm-tuning-key) {
    color: #93c5fd;
  }
  .tuning-editor :global(.cm-tuning-section) {
    color: #7c3aed;
    font-weight: 600;
  }
  :global(.dark) .tuning-editor :global(.cm-tuning-section) {
    color: #c4b5fd;
  }
  .tuning-editor :global(.cm-tuning-op) {
    color: #9ca3af;
  }
  .tuning-editor :global(.cm-tuning-value) {
    color: #374151;
  }
  :global(.dark) .tuning-editor :global(.cm-tuning-value) {
    color: #e5e7eb;
  }
  .tuning-editor :global(.cm-tuning-comment) {
    color: #9ca3af;
    font-style: italic;
  }
</style>
