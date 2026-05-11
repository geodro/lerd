import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { get } from 'svelte/store';

class MockEventSource {
  static instances: MockEventSource[] = [];
  url: string;
  listeners: Record<string, ((e: unknown) => void)[]> = {};
  closed = false;

  constructor(url: string) {
    this.url = url;
    MockEventSource.instances.push(this);
  }

  addEventListener(ev: string, fn: (e: unknown) => void) {
    (this.listeners[ev] ||= []).push(fn);
  }

  fire(ev: string, data: unknown) {
    for (const fn of this.listeners[ev] || []) fn(data);
  }

  close() {
    this.closed = true;
  }
}

function payload(id: string, extra: Record<string, unknown> = {}) {
  return JSON.stringify({
    v: 1,
    id,
    ts: '2026-05-10T00:00:00.000Z',
    kind: 'dump',
    ctx: { type: 'fpm', site: 'acme' },
    src: { file: '/x.php', line: 1 },
    text: 'value',
    ...extra
  });
}

describe('createDumpsStream', () => {
  const realES = globalThis.EventSource;

  beforeEach(() => {
    MockEventSource.instances = [];
    // @ts-expect-error test double
    globalThis.EventSource = MockEventSource;
  });

  afterEach(() => {
    globalThis.EventSource = realES;
  });

  it('parses message events into typed entries', async () => {
    const { createDumpsStream } = await import('./dumpsStream');
    const s = createDumpsStream();
    s.connect();
    MockEventSource.instances[0].fire('message', { data: payload('a') });
    MockEventSource.instances[0].fire('message', { data: payload('b') });
    const list = get(s.events);
    expect(list.map((e) => e.id)).toEqual(['a', 'b']);
    expect(list[0].ctx.site).toBe('acme');
  });

  it('de-dupes by id (replay safety)', async () => {
    const { createDumpsStream } = await import('./dumpsStream');
    const s = createDumpsStream();
    s.connect();
    MockEventSource.instances[0].fire('message', { data: payload('a') });
    MockEventSource.instances[0].fire('message', { data: payload('a') });
    expect(get(s.events).length).toBe(1);
  });

  it('caps at maxEvents (drops oldest)', async () => {
    const { createDumpsStream } = await import('./dumpsStream');
    const s = createDumpsStream({}, 3);
    s.connect();
    for (let i = 0; i < 5; i++) {
      MockEventSource.instances[0].fire('message', { data: payload('e' + i) });
    }
    expect(get(s.events).map((e) => e.id)).toEqual(['e2', 'e3', 'e4']);
  });

  it('skips malformed JSON without throwing', async () => {
    const { createDumpsStream } = await import('./dumpsStream');
    const s = createDumpsStream();
    s.connect();
    MockEventSource.instances[0].fire('message', { data: '{not valid json' });
    MockEventSource.instances[0].fire('message', { data: payload('ok') });
    expect(get(s.events).map((e) => e.id)).toEqual(['ok']);
  });

  it('builds query string from filters', async () => {
    const { createDumpsStream } = await import('./dumpsStream');
    const s = createDumpsStream({ site: 'acme', ctx: 'fpm' });
    s.connect();
    expect(MockEventSource.instances[0].url).toContain('site=acme');
    expect(MockEventSource.instances[0].url).toContain('ctx=fpm');
  });

  it('clear empties the events store', async () => {
    const { createDumpsStream } = await import('./dumpsStream');
    const s = createDumpsStream();
    s.connect();
    MockEventSource.instances[0].fire('message', { data: payload('a') });
    s.clear();
    expect(get(s.events)).toEqual([]);
  });

  it('close tears down the EventSource', async () => {
    const { createDumpsStream } = await import('./dumpsStream');
    const s = createDumpsStream();
    s.connect();
    s.close();
    expect(MockEventSource.instances[0].closed).toBe(true);
    expect(get(s.connected)).toBe(false);
  });

  it('open fires connected=true', async () => {
    const { createDumpsStream } = await import('./dumpsStream');
    const s = createDumpsStream();
    s.connect();
    MockEventSource.instances[0].fire('open', {});
    expect(get(s.connected)).toBe(true);
  });

  it('error fires connected=false', async () => {
    const { createDumpsStream } = await import('./dumpsStream');
    const s = createDumpsStream();
    s.connect();
    MockEventSource.instances[0].fire('open', {});
    MockEventSource.instances[0].fire('error', {});
    expect(get(s.connected)).toBe(false);
  });
});
