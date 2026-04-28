import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

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

describe('streamLinkSite', () => {
  const realFetch = globalThis.fetch;
  beforeEach(() => vi.resetModules());
  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  it('parses event/data lines and surfaces done payload', async () => {
    const body = readerFrom([
      'data: line one\n',
      '\n',
      'data: line two\n',
      '\n',
      'event: done\ndata: {"ok":true,"domain":"a.test"}\n'
    ]);
    globalThis.fetch = vi.fn(async () => new Response(body, { status: 200 })) as unknown as typeof fetch;
    const { streamLinkSite } = await import('./link');
    const events: Array<{ line?: string; done?: boolean; domain?: string; ok?: boolean }> = [];
    await streamLinkSite('/path', (e) => events.push(e));
    expect(events).toHaveLength(3);
    expect(events[0].line).toBe('line one');
    expect(events[1].line).toBe('line two');
    expect(events[2].done).toBe(true);
    expect(events[2].ok).toBe(true);
    expect(events[2].domain).toBe('a.test');
  });

  it('surfaces done error payload', async () => {
    const body = readerFrom(['event: done\ndata: {"ok":false,"error":"nope"}\n']);
    globalThis.fetch = vi.fn(async () => new Response(body, { status: 200 })) as unknown as typeof fetch;
    const { streamLinkSite } = await import('./link');
    const events: Array<{ ok?: boolean; error?: string; done?: boolean }> = [];
    await streamLinkSite('/p', (e) => events.push(e));
    expect(events[0].done).toBe(true);
    expect(events[0].ok).toBe(false);
    expect(events[0].error).toBe('nope');
  });
});
