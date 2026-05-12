import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, cleanup, waitFor } from '@testing-library/svelte';
import { get } from 'svelte/store';
import DumpsTab from './DumpsTab.svelte';
import {
  dumps,
  filterSite,
  filterCtx,
  filterText,
  status
} from '../stores/dumps';
import type { DumpEvent } from '../lib/dumpsStream';

function ev(over: Partial<DumpEvent> & { id: string }): DumpEvent {
  return {
    v: 1,
    id: over.id,
    ts: over.ts ?? '2026-05-10T12:00:00.000Z',
    kind: 'dump',
    ctx: over.ctx ?? { type: 'fpm', site: 'whitewaters', request: 'GET /' },
    src: over.src ?? { file: '/app/foo.php', line: 12 },
    text: over.text ?? 'array:1 [\n  "k" => "v"\n]\n'
  };
}

// MockEventSource keeps connect() from actually opening a network socket
// during the component's onMount.
class MockEventSource {
  url: string;
  listeners: Record<string, ((e: unknown) => void)[]> = {};
  closed = false;
  constructor(url: string) {
    this.url = url;
  }
  addEventListener(ev: string, fn: (e: unknown) => void) {
    (this.listeners[ev] ||= []).push(fn);
  }
  close() {
    this.closed = true;
  }
}

describe('DumpsTab', () => {
  const realES = globalThis.EventSource;
  const realFetch = globalThis.fetch;

  beforeEach(() => {
    dumps.set([]);
    filterSite.set('');
    filterCtx.set('');
    filterText.set('');
    status.set({
      enabled: true,
      passthrough: false,
      listening: true,
      addr: 'unix:/tmp/x',
      count: 0,
      subscribers: 0,
      last_ts: ''
    });
    // @ts-expect-error test double
    globalThis.EventSource = MockEventSource;
    globalThis.fetch = vi.fn(async () => new Response(JSON.stringify({ enabled: true }), { status: 200 })) as unknown as typeof fetch;
  });

  afterEach(() => {
    cleanup();
    globalThis.EventSource = realES;
    globalThis.fetch = realFetch;
  });

  it('renders dump events that match siteScope', async () => {
    dumps.set([
      ev({ id: 'a', ctx: { type: 'fpm', site: 'whitewaters', request: 'GET /matched' } }),
      ev({ id: 'b', ctx: { type: 'fpm', site: 'otherone', request: 'GET /excluded' } })
    ]);
    const { container } = render(DumpsTab, { siteScope: 'whitewaters' });
    await waitFor(() => {
      // Scoped view drops the [site] prefix — assert the request URL of
      // the matching event is visible and the excluded one isn't.
      expect(container.textContent).toContain('GET /matched');
    });
    expect(container.textContent).not.toContain('GET /excluded');
    expect(container.textContent).not.toContain('[whitewaters]');
  });

  it('shows empty state when no events match scope', async () => {
    dumps.set([
      ev({ id: 'a', ctx: { type: 'fpm', site: 'someone-else', request: 'GET /' } })
    ]);
    const { container } = render(DumpsTab, { siteScope: 'whitewaters' });
    await waitFor(() => {
      expect(container.textContent).toMatch(/Waiting for dumps/);
    });
  });

  it('does not mutate global filterSite when scoped', async () => {
    filterSite.set('previously-selected');
    dumps.set([ev({ id: 'a', ctx: { type: 'fpm', site: 'whitewaters' } })]);
    render(DumpsTab, { siteScope: 'whitewaters' });
    // Give onMount + effects a chance to run.
    await new Promise((r) => setTimeout(r, 20));
    expect(get(filterSite)).toBe('previously-selected');
  });

  it('shows an Enable button when the bridge is off and the ring-3 is empty', async () => {
    status.set({
      enabled: false,
      passthrough: false,
      listening: true,
      addr: 'unix:/tmp/x',
      count: 0,
      subscribers: 0,
      last_ts: ''
    });
    const { container } = render(DumpsTab, { siteScope: 'whitewaters' });
    await waitFor(() => {
      expect(container.textContent).toMatch(/Enable dump bridge/);
    });
    expect(container.textContent).toMatch(/Dump bridge is disabled/);
  });

  it('reacts to new events pushed into the dumps store', async () => {
    const { container } = render(DumpsTab, { siteScope: 'whitewaters' });
    expect(container.textContent).toMatch(/Waiting for dumps/);

    dumps.update((arr) => [...arr, ev({ id: 'live', ctx: { type: 'fpm', site: 'whitewaters', request: 'GET /live' } })]);
    await waitFor(() => {
      expect(container.textContent).toContain('GET /live');
    });
  });
});
