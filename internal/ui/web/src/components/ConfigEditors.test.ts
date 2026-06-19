import { render } from '@testing-library/svelte';
import { describe, it, expect, vi } from 'vitest';
import NginxEditor from './NginxEditor.svelte';
import TuningEditor from './TuningEditor.svelte';
import EnvEditor from './EnvEditor.svelte';

// Monaco can't run in jsdom; mock the loader and capture the language each
// config wrapper asks the editor to open with.
const { state } = vi.hoisted(() => ({
  state: { createOpts: null as any, readOnly: false as boolean }
}));

vi.mock('$lib/monaco', () => {
  const monaco = {
    editor: {
      create: (_el: HTMLElement, opts: any) => {
        state.createOpts = opts;
        state.readOnly = !!opts.readOnly;
        return {
          getValue: () => opts.value ?? '',
          setValue: () => {},
          onDidChangeModelContent: () => ({ dispose() {} }),
          updateOptions: (o: any) => { if (o.readOnly !== undefined) state.readOnly = o.readOnly; },
          dispose: () => {}
        };
      },
      setTheme: () => {},
      defineTheme: () => {}
    }
  };
  return { loadMonaco: () => Promise.resolve(monaco), lerdThemeName: () => 'lerd-dark' };
});

const flush = () => new Promise((r) => setTimeout(r, 0));

describe('config editors', () => {
  it('NginxEditor opens the nginx language', async () => {
    render(NginxEditor, { props: { value: 'server { }' } });
    await flush();
    expect(state.createOpts.language).toBe('nginx');
    expect(state.createOpts.value).toBe('server { }');
  });

  it('TuningEditor opens the ini language', async () => {
    render(TuningEditor, { props: { value: '[mysqld]' } });
    await flush();
    expect(state.createOpts.language).toBe('ini');
  });

  it('EnvEditor opens the dotenv language', async () => {
    render(EnvEditor, { props: { value: 'APP_ENV=local' } });
    await flush();
    expect(state.createOpts.language).toBe('dotenv');
  });

  it('forwards readOnly', async () => {
    render(EnvEditor, { props: { value: 'X=1', readOnly: true } });
    await flush();
    expect(state.readOnly).toBe(true);
  });
});
