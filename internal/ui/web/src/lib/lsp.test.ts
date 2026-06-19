import { describe, it, expect } from 'vitest';
import {
  lspWorkspaceEditToMonaco,
  isBlankCompletionPrefix,
  stripSyntheticHeader,
  withImportBlankLine
} from '$lib/lsp';

// Mirrors the production fromLspRange: LSP 0-based -> Monaco 1-based, line 0
// clamped to 1 (the synthetic <?php line).
const fromLspRange = (r: any) => ({
  startLineNumber: Math.max(1, r.start.line),
  startColumn: r.start.character + 1,
  endLineNumber: Math.max(1, r.end.line),
  endColumn: r.end.character + 1
});

const URI = 'file:///home/me/proj/.lerd-tinker.php';
const importEdit = {
  range: { start: { line: 1, character: 0 }, end: { line: 1, character: 0 } },
  newText: 'use App\\Models\\Bid;\n'
};

describe('lspWorkspaceEditToMonaco', () => {
  it('returns [] for a null edit', () => {
    expect(lspWorkspaceEditToMonaco(null, URI, fromLspRange)).toEqual([]);
  });

  it('converts a `changes` map edit for our document into Monaco coordinates', () => {
    const edit = { changes: { [URI]: [importEdit] } };
    expect(lspWorkspaceEditToMonaco(edit, URI, fromLspRange)).toEqual([
      {
        range: { startLineNumber: 1, startColumn: 1, endLineNumber: 1, endColumn: 1 },
        text: 'use App\\Models\\Bid;\n'
      }
    ]);
  });

  it('handles the documentChanges shape', () => {
    const edit = { documentChanges: [{ textDocument: { uri: URI, version: 1 }, edits: [importEdit] }] };
    expect(lspWorkspaceEditToMonaco(edit, URI, fromLspRange)).toHaveLength(1);
  });

  it('ignores edits for other documents', () => {
    const edit = { changes: { 'file:///other.php': [importEdit] } };
    expect(lspWorkspaceEditToMonaco(edit, URI, fromLspRange)).toEqual([]);
  });

  it('matches URIs regardless of percent-encoding', () => {
    const encoded = 'file:///home/me/my%20proj/.lerd-tinker.php';
    const decoded = 'file:///home/me/my proj/.lerd-tinker.php';
    const edit = { changes: { [encoded]: [importEdit] } };
    expect(lspWorkspaceEditToMonaco(edit, decoded, fromLspRange)).toHaveLength(1);
  });

  it('defaults a missing newText to an empty string and skips rangeless edits', () => {
    const edit = {
      changes: {
        [URI]: [
          { range: importEdit.range },
          { newText: 'x' }
        ]
      }
    };
    const out = lspWorkspaceEditToMonaco(edit, URI, fromLspRange);
    expect(out).toEqual([
      { range: { startLineNumber: 1, startColumn: 1, endLineNumber: 1, endColumn: 1 }, text: '' }
    ]);
  });
});

describe('withImportBlankLine', () => {
  const at1 = (text: string) => () => text;
  const insertAt1 = (text: string) => ({
    range: { startLineNumber: 1, startColumn: 1, endLineNumber: 1, endColumn: 1 },
    text
  });

  it('adds a blank line when the import sits directly above code', () => {
    const out = withImportBlankLine(insertAt1('use App\\Models\\Bid;\n'), at1('Bid::query()->first();'));
    expect(out.text).toBe('use App\\Models\\Bid;\n\n');
  });

  it('does not add a blank line when the following line is already blank', () => {
    const out = withImportBlankLine(insertAt1('use App\\Models\\Bid;\n'), at1(''));
    expect(out.text).toBe('use App\\Models\\Bid;\n');
  });

  it('keeps imports grouped (no blank) when the next line is another use', () => {
    const out = withImportBlankLine(insertAt1('use App\\Models\\Bid;\n'), at1('use App\\Models\\Category;'));
    expect(out.text).toBe('use App\\Models\\Bid;\n');
  });

  it('leaves non-import edits untouched', () => {
    const out = withImportBlankLine(insertAt1('Bid'), at1('whatever'));
    expect(out.text).toBe('Bid');
  });

  it('does not double a blank line that is already present', () => {
    const out = withImportBlankLine(insertAt1('use App\\Models\\Bid;\n\n'), at1('Bid::query();'));
    expect(out.text).toBe('use App\\Models\\Bid;\n\n');
  });
});

describe('isBlankCompletionPrefix', () => {
  it('suppresses on an empty or whitespace-only line', () => {
    expect(isBlankCompletionPrefix('')).toBe(true);
    expect(isBlankCompletionPrefix('   ')).toBe(true);
    expect(isBlankCompletionPrefix('\t')).toBe(true);
  });

  it('allows once anything has been typed', () => {
    expect(isBlankCompletionPrefix('Bi')).toBe(false);
    expect(isBlankCompletionPrefix('  $user->')).toBe(false);
    expect(isBlankCompletionPrefix('Bid::')).toBe(false);
  });
});

describe('stripSyntheticHeader', () => {
  it('drops the <?php line and the blank line the formatter adds', () => {
    const formatted = '<?php\n\n$x = Bid::query()->first();\nif ($x) {\n    echo $x->id;\n}\n';
    expect(stripSyntheticHeader(formatted)).toBe('$x = Bid::query()->first();\nif ($x) {\n    echo $x->id;\n}\n');
  });

  it('drops the <?php line even with no following blank line', () => {
    expect(stripSyntheticHeader('<?php\n$x = 1;\n')).toBe('$x = 1;\n');
  });

  it('leaves headerless text untouched', () => {
    expect(stripSyntheticHeader('$x = 1;\n')).toBe('$x = 1;\n');
  });
});
