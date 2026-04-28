import { describe, it, expect } from 'vitest';
import { parseHash, goToTab, TABS } from './route';

describe('parseHash', () => {
  it('defaults to sites for empty hash', () => {
    expect(parseHash('')).toEqual({ tab: 'sites', rest: '' });
    expect(parseHash('#')).toEqual({ tab: 'sites', rest: '' });
  });

  it('parses bare tab', () => {
    for (const t of TABS) {
      expect(parseHash('#' + t)).toEqual({ tab: t, rest: '' });
      expect(parseHash(t)).toEqual({ tab: t, rest: '' });
    }
  });

  it('parses nested route', () => {
    expect(parseHash('#services/mysql')).toEqual({ tab: 'services', rest: 'mysql' });
    expect(parseHash('#sites/foo.test/logs')).toEqual({ tab: 'sites', rest: 'foo.test/logs' });
  });

  it('falls back to sites for unknown tab', () => {
    expect(parseHash('#nope')).toEqual({ tab: 'sites', rest: '' });
  });
});

describe('goToTab', () => {
  it('writes bare tab to hash', () => {
    goToTab('system');
    expect(location.hash).toBe('#system');
  });

  it('writes tab with rest', () => {
    goToTab('sites', 'foo.test');
    expect(location.hash).toBe('#sites/foo.test');
  });
});
