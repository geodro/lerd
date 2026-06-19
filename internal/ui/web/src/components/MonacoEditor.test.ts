import { render } from '@testing-library/svelte';
import { describe, it, expect, vi } from 'vitest';
import Harness from './MonacoEditor.test.svelte';

// Monaco needs canvas/layout/workers that jsdom lacks, so we mock the
// lazy loader and assert MonacoEditor's wiring against a fake editor.
const { state } = vi.hoisted(() => ({
  state: {
    createOpts: null as any,
    changeHandler: null as (() => void) | null,
    value: '',
    readOnly: false as boolean,
    addCommandCalls: 0,
    disposed: false
  }
}));

vi.mock('$lib/monaco', () => {
  const fakeEditor = {
    getValue: () => state.value,
    setValue: (v: string) => { state.value = v; },
    onDidChangeModelContent: (cb: () => void) => { state.changeHandler = cb; return { dispose() {} }; },
    updateOptions: (o: { readOnly?: boolean }) => { if (o.readOnly !== undefined) state.readOnly = o.readOnly; },
    addCommand: () => { state.addCommandCalls++; },
    dispose: () => { state.disposed = true; }
  };
  const monaco = {
    editor: {
      create: (_el: HTMLElement, opts: any) => {
        state.createOpts = opts;
        state.value = opts.value ?? '';
        state.readOnly = !!opts.readOnly;
        return fakeEditor;
      },
      setTheme: () => {},
      defineTheme: () => {}
    },
    KeyMod: { CtrlCmd: 2048 },
    KeyCode: { Enter: 3 }
  };
  return {
    loadMonaco: () => Promise.resolve(monaco),
    lerdThemeName: () => 'lerd-dark'
  };
});

function flush() {
  // onMount's async loader resolves on the microtask queue.
  return new Promise((r) => setTimeout(r, 0));
}

describe('MonacoEditor', () => {
  it('creates an editor seeded with the value and language', async () => {
    render(Harness, { props: { value: 'echo 1;', language: 'php' } });
    await flush();
    expect(state.createOpts).toBeTruthy();
    expect(state.createOpts.value).toBe('echo 1;');
    expect(state.createOpts.language).toBe('php');
  });

  it('reports editor edits back through the bound value', async () => {
    const onChange = vi.fn();
    render(Harness, { props: { value: '', onChange } });
    await flush();
    state.value = 'typed';
    state.changeHandler?.();
    expect(onChange).toHaveBeenCalledWith('typed');
  });

  it('passes readOnly into the editor', async () => {
    render(Harness, { props: { value: 'x', readOnly: true } });
    await flush();
    expect(state.readOnly).toBe(true);
  });

  it('hands the editor and monaco to onReady', async () => {
    const onReady = vi.fn();
    render(Harness, { props: { value: 'x', onReady } });
    await flush();
    expect(onReady).toHaveBeenCalledOnce();
    const arg = onReady.mock.calls[0][0];
    expect(arg.editor).toBeTruthy();
    expect(arg.monaco).toBeTruthy();
  });
});
