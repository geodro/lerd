import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { get } from 'svelte/store';

class MockWebSocket {
  static OPEN = 1;
  static CONNECTING = 0;
  static CLOSED = 3;
  static instances: MockWebSocket[] = [];

  readyState = MockWebSocket.CONNECTING;
  url: string;
  listeners: Record<string, ((e: unknown) => void)[]> = {};
  sent: string[] = [];

  constructor(url: string) {
    this.url = url;
    MockWebSocket.instances.push(this);
  }

  addEventListener(ev: string, fn: (e: unknown) => void) {
    (this.listeners[ev] ||= []).push(fn);
  }

  send(data: string) {
    this.sent.push(data);
  }

  close() {
    this.readyState = MockWebSocket.CLOSED;
    this.fire('close', {});
  }

  fire(ev: string, data: unknown) {
    for (const fn of this.listeners[ev] || []) fn(data);
  }
}

describe('ws client', () => {
  const realWS = globalThis.WebSocket;

  beforeEach(() => {
    MockWebSocket.instances = [];
    // @ts-expect-error test double
    globalThis.WebSocket = MockWebSocket;
    vi.useFakeTimers();
    vi.resetModules();
  });

  afterEach(() => {
    globalThis.WebSocket = realWS;
    vi.useRealTimers();
  });

  it('connects and exposes wsConnected', async () => {
    const { connectWs, wsConnected, disconnectWs } = await import('./ws');
    connectWs();
    expect(MockWebSocket.instances).toHaveLength(1);
    expect(get(wsConnected)).toBe(false);
    MockWebSocket.instances[0].readyState = MockWebSocket.OPEN;
    MockWebSocket.instances[0].fire('open', {});
    expect(get(wsConnected)).toBe(true);
    disconnectWs();
  });

  it('parses message frames into wsMessage', async () => {
    const { connectWs, wsMessage, disconnectWs } = await import('./ws');
    connectWs();
    MockWebSocket.instances[0].fire('message', {
      data: JSON.stringify({ type: 'snapshot', sites: [{ domain: 'a.test' }] })
    });
    const m = get(wsMessage);
    expect(m?.type).toBe('snapshot');
    expect((m?.sites as Array<{ domain: string }>)[0].domain).toBe('a.test');
    disconnectWs();
  });

  it('ignores malformed frames', async () => {
    const { connectWs, wsMessage, disconnectWs } = await import('./ws');
    connectWs();
    MockWebSocket.instances[0].fire('message', { data: 'not-json' });
    expect(get(wsMessage)).toBeNull();
    disconnectWs();
  });

  it('reconnects with backoff after close', async () => {
    const { connectWs, disconnectWs } = await import('./ws');
    connectWs();
    MockWebSocket.instances[0].close();
    expect(MockWebSocket.instances).toHaveLength(1);
    vi.advanceTimersByTime(1000);
    expect(MockWebSocket.instances).toHaveLength(2);
    disconnectWs();
  });
});
