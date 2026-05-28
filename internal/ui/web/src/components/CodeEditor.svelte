<script lang="ts">
  import { onMount } from 'svelte';
  import { EditorView } from '@codemirror/view';
  import { EditorState, Compartment, type Extension } from '@codemirror/state';

  interface Props {
    value: string;
    extensions?: Extension[];
    onChange?: (next: string) => void;
    readOnly?: boolean;
    /** Extra theme overrides merged on top of the default editor theme. */
    themeOverrides?: Parameters<typeof EditorView.theme>[0];
    /** Optional extra classes on the editor container. */
    class?: string;
  }
  let {
    value = $bindable(''),
    extensions = [],
    onChange,
    readOnly = false,
    themeOverrides = {},
    class: extraClass = ''
  }: Props = $props();

  let container: HTMLDivElement | undefined = $state();
  let view: EditorView | undefined;
  let internalUpdate = false;
  const readOnlyCompartment = new Compartment();

  onMount(() => {
    if (!container) return;
    view = new EditorView({
      parent: container,
      state: EditorState.create({
        doc: value,
        extensions: [
          ...extensions,
          readOnlyCompartment.of(EditorState.readOnly.of(readOnly)),
          EditorView.updateListener.of((u) => {
            // Skip onChange for programmatic dispatches triggered by the
            // $effect below; those carry external value writes that the
            // parent already knows about. Without this guard, every
            // external value update would feed back into onChange and
            // look indistinguishable from user input.
            if (!u.docChanged || internalUpdate) return;
            const next = u.state.doc.toString();
            value = next;
            onChange?.(next);
          }),
          EditorView.theme({
            '&': { height: '100%', fontSize: '12px' },
            '.cm-scroller': { fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace' },
            '.cm-content': { paddingTop: '8px', paddingBottom: '32px' },
            ...themeOverrides
          })
        ]
      })
    });
    return () => view?.destroy();
  });

  // Mirror external value mutations into the editor without re-entering
  // the updateListener that drove the change in the first place.
  $effect(() => {
    if (!view) return;
    const current = view.state.doc.toString();
    if (current !== value) {
      internalUpdate = true;
      try {
        view.dispatch({ changes: { from: 0, to: current.length, insert: value } });
      } finally {
        internalUpdate = false;
      }
    }
  });

  // Re-configure readOnly via Compartment so a prop change after mount
  // actually flips the editor's writability instead of being ignored.
  $effect(() => {
    if (!view) return;
    view.dispatch({
      effects: readOnlyCompartment.reconfigure(EditorState.readOnly.of(readOnly))
    });
  });
</script>

<div bind:this={container} class="h-full w-full {extraClass}"></div>
