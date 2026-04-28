import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
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

describe('createLogStream', () => {
  const realES = globalThis.EventSource;

  beforeEach(() => {
    MockEventSource.instances = [];
    // @ts-expect-error test double
    globalThis.EventSource = MockEventSource;
  });

  afterEach(() => {
    globalThis.EventSource = realES;
  });

  it('collects message lines into lines store', async () => {
    const { createLogStream } = await import('./logStream');
    const s = createLogStream('/api/logs/x');
    s.connect();
    MockEventSource.instances[0].fire('open', {});
    expect(get(s.connected)).toBe(true);
    MockEventSource.instances[0].fire('message', { data: 'line 1' });
    MockEventSource.instances[0].fire('message', { data: 'line 2' });
    expect(get(s.lines)).toEqual(['line 1', 'line 2']);
  });

  it('caps lines at maxLines (dropping oldest)', async () => {
    const { createLogStream } = await import('./logStream');
    const s = createLogStream('/api/logs/x', 3);
    s.connect();
    for (let i = 0; i < 5; i++) {
      MockEventSource.instances[0].fire('message', { data: 'l' + i });
    }
    expect(get(s.lines)).toEqual(['l2', 'l3', 'l4']);
  });

  it('clear empties the buffer', async () => {
    const { createLogStream } = await import('./logStream');
    const s = createLogStream('/api/logs/x');
    s.connect();
    MockEventSource.instances[0].fire('message', { data: 'x' });
    s.clear();
    expect(get(s.lines)).toEqual([]);
  });

  it('close tears down the EventSource', async () => {
    const { createLogStream } = await import('./logStream');
    const s = createLogStream('/api/logs/x');
    s.connect();
    s.close();
    expect(MockEventSource.instances[0].closed).toBe(true);
    expect(get(s.connected)).toBe(false);
  });

  it('sets connected=false on error', async () => {
    const { createLogStream } = await import('./logStream');
    const s = createLogStream('/api/logs/x');
    s.connect();
    MockEventSource.instances[0].fire('open', {});
    MockEventSource.instances[0].fire('error', {});
    expect(get(s.connected)).toBe(false);
  });
});
