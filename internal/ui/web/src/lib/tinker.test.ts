import { describe, it, expect } from 'vitest';
import { peelBlockMarker, parseBlock } from '$lib/tinker';

const US = String.fromCharCode(0x1f);
const STX = String.fromCharCode(0x02);

describe('peelBlockMarker', () => {
  it('extracts the line and strips the marker', () => {
    expect(peelBlockMarker(`9${US}hello`)).toEqual({ line: 9, rest: 'hello' });
    expect(peelBlockMarker(`12${US}`)).toEqual({ line: 12, rest: '' });
  });

  it('leaves a markerless chunk untouched', () => {
    expect(peelBlockMarker('hello')).toEqual({ rest: 'hello' });
  });

  it('does not treat plain numeric output as a marker', () => {
    expect(peelBlockMarker('42')).toEqual({ rest: '42' });
    expect(peelBlockMarker('42\n')).toEqual({ rest: '42\n' });
  });
});

describe('parseBlock', () => {
  it('classifies a statement-output block', () => {
    expect(parseBlock(`2${US}hello`)).toEqual({ line: 2, kind: 'output', body: 'hello' });
  });

  it('classifies a query block and strips the STX marker', () => {
    expect(parseBlock(`5${US}${STX}select * from users`)).toEqual({
      line: 5,
      kind: 'query',
      body: 'select * from users'
    });
  });

  it('handles a markerless chunk as output', () => {
    expect(parseBlock('Deprecated: ...')).toEqual({ kind: 'output', body: 'Deprecated: ...' });
  });
});
