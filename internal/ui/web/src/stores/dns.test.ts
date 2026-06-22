import { describe, it, expect } from 'vitest';
import { isValidUpstream } from './dns';

describe('isValidUpstream', () => {
  it('accepts IPv4 with and without a port', () => {
    expect(isValidUpstream('192.168.100.129')).toBe(true);
    expect(isValidUpstream('  8.8.8.8  ')).toBe(true);
    expect(isValidUpstream('1.1.1.1#5353')).toBe(true);
  });

  it('accepts IPv6 (full and compressed) with and without a port', () => {
    expect(isValidUpstream('2001:db8::1')).toBe(true);
    expect(isValidUpstream('::1')).toBe(true);
    expect(isValidUpstream('fe80::1#53')).toBe(true);
    expect(isValidUpstream('2001:0db8:0000:0000:0000:0000:0000:0001')).toBe(true);
  });

  it('rejects malformed addresses', () => {
    expect(isValidUpstream('')).toBe(false);
    expect(isValidUpstream('not-an-ip')).toBe(false);
    expect(isValidUpstream('1.2.3')).toBe(false);
    expect(isValidUpstream('256.0.0.1')).toBe(false);
    expect(isValidUpstream('2001:db8:::1')).toBe(false);
    expect(isValidUpstream('gggg::1')).toBe(false);
  });

  it('rejects bad ports', () => {
    expect(isValidUpstream('8.8.8.8#0')).toBe(false);
    expect(isValidUpstream('8.8.8.8#70000')).toBe(false);
    expect(isValidUpstream('8.8.8.8#abc')).toBe(false);
    expect(isValidUpstream('8.8.8.8#')).toBe(false);
  });
});
