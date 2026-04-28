import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { get } from 'svelte/store';

function readerFrom(chunks: string[]): ReadableStream<Uint8Array> {
  const enc = new TextEncoder();
  let i = 0;
  return new ReadableStream<Uint8Array>({
    pull(ctrl) {
      if (i >= chunks.length) {
        ctrl.close();
        return;
      }
      ctrl.enqueue(enc.encode(chunks[i++]));
    }
  });
}

describe('presets store', () => {
  const realFetch = globalThis.fetch;

  beforeEach(() => {
    vi.resetModules();
  });

  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  it('loadPresets populates with default selected_version', async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response(
        JSON.stringify([
          {
            name: 'mysql',
            versions: [{ tag: '5.7' }, { tag: '8.0' }],
            default_version: '8.0',
            installed_tags: []
          }
        ]),
        { status: 200 }
      )
    ) as unknown as typeof fetch;
    const { presets, loadPresets } = await import('./presets');
    await loadPresets();
    const list = get(presets);
    expect(list[0].selected_version).toBe('8.0');
  });

  it('installablePresets hides presets with missing deps', async () => {
    const { presets, installablePresets } = await import('./presets');
    presets.set([
      { name: 'a', site_count: 0 } as never,
      { name: 'b', missing_deps: ['x'], site_count: 0 } as never
    ]);
    expect(get(installablePresets).map((p) => p.name)).toEqual(['a']);
  });

  it('installablePresets only shows versioned when new versions exist', async () => {
    const { presets, installablePresets } = await import('./presets');
    presets.set([
      { name: 'full', versions: [{ tag: '1' }], installed_tags: ['1'] } as never,
      { name: 'part', versions: [{ tag: '1' }, { tag: '2' }], installed_tags: ['1'] } as never
    ]);
    expect(get(installablePresets).map((p) => p.name)).toEqual(['part']);
  });

  it('phaseLabel maps installing phases', async () => {
    const { phaseLabel } = await import('./presets');
    expect(phaseLabel({ name: 'x' } as never)).toBe('Add');
    expect(phaseLabel({ name: 'x', installing: true } as never)).toBe('Adding...');
    expect(phaseLabel({ name: 'x', installing: true, installingPhase: 'pulling_image' } as never)).toBe(
      'Pulling image...'
    );
  });

  it('installPreset streams NDJSON events and sets phases', async () => {
    const body = readerFrom([
      '{"phase":"pulling_image","image":"mysql:8"}\n',
      '{"phase":"starting_unit"}\n',
      '{"phase":"done","name":"mysql"}\n'
    ]);
    globalThis.fetch = vi.fn(async () => new Response(body, { status: 200 })) as unknown as typeof fetch;
    const { installPreset } = await import('./presets');
    const r = await installPreset({ name: 'mysql', site_count: 0 } as never);
    expect(r.ok).toBe(true);
    expect(r.name).toBe('mysql');
  });

  it('installPreset surfaces error from final event', async () => {
    const body = readerFrom(['{"phase":"error","error":"boom"}\n']);
    globalThis.fetch = vi.fn(async () => new Response(body, { status: 200 })) as unknown as typeof fetch;
    const { installPreset } = await import('./presets');
    const r = await installPreset({ name: 'bad' } as never);
    expect(r.ok).toBe(false);
    expect(r.error).toBe('boom');
  });

  it('installPreset short-circuits when missing_deps is non-empty', async () => {
    globalThis.fetch = vi.fn(async () => {
      throw new Error('should not be called');
    }) as unknown as typeof fetch;
    const { installPreset } = await import('./presets');
    const r = await installPreset({ name: 'x', missing_deps: ['mysql'] } as never);
    expect(r.ok).toBe(false);
    expect(r.error).toMatch(/mysql first/);
  });
});
