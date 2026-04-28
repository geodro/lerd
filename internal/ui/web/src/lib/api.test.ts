import { describe, it, expect } from 'vitest';
import { apiUrl, wsUrl } from './api';

describe('apiUrl', () => {
  it('passes absolute URLs through', () => {
    expect(apiUrl('https://example.com/foo')).toBe('https://example.com/foo');
    expect(apiUrl('http://x/y')).toBe('http://x/y');
  });

  it('prepends apiBase only when non-empty', () => {
    // default hostname in jsdom is "localhost", so apiBase is ''
    expect(apiUrl('/api/version')).toBe('/api/version');
  });
});

describe('wsUrl', () => {
  it('rewrites http to ws', () => {
    const u = wsUrl('/api/ws');
    expect(u.startsWith('ws://') || u.startsWith('wss://')).toBe(true);
    expect(u.endsWith('/api/ws')).toBe(true);
  });
});
