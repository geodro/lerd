import { describe, it, expect } from 'vitest';
import { diffLines } from './diff';

describe('diffLines', () => {
  it('marks every line as context when inputs match', () => {
    const out = diffLines('A\nB\nC', 'A\nB\nC');
    expect(out).toEqual([
      { op: ' ', line: 'A' },
      { op: ' ', line: 'B' },
      { op: ' ', line: 'C' }
    ]);
  });

  it('emits a single insertion', () => {
    const out = diffLines('A\nC', 'A\nB\nC');
    expect(out).toEqual([
      { op: ' ', line: 'A' },
      { op: '+', line: 'B' },
      { op: ' ', line: 'C' }
    ]);
  });

  it('emits a single deletion', () => {
    const out = diffLines('A\nB\nC', 'A\nC');
    expect(out).toEqual([
      { op: ' ', line: 'A' },
      { op: '-', line: 'B' },
      { op: ' ', line: 'C' }
    ]);
  });

  it('emits a single replacement as delete + insert', () => {
    const out = diffLines('DB_HOST=localhost', 'DB_HOST=127.0.0.1');
    expect(out).toEqual([
      { op: '-', line: 'DB_HOST=localhost' },
      { op: '+', line: 'DB_HOST=127.0.0.1' }
    ]);
  });

  it('treats empty input as fully inserted / deleted', () => {
    expect(diffLines('', 'A\nB')).toEqual([
      { op: '+', line: 'A' },
      { op: '+', line: 'B' }
    ]);
    expect(diffLines('A\nB', '')).toEqual([
      { op: '-', line: 'A' },
      { op: '-', line: 'B' }
    ]);
  });

  it('returns an empty array for identical empty inputs', () => {
    expect(diffLines('', '')).toEqual([]);
  });
});
