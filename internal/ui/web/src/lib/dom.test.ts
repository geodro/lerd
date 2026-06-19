import { describe, it, expect } from 'vitest';
import { isEditableTarget } from './dom';

describe('isEditableTarget', () => {
  it('treats inputs and textareas as editable', () => {
    // contenteditable is handled via t.isContentEditable in real browsers;
    // jsdom doesn't compute it from the attribute, so it isn't asserted here.
    expect(isEditableTarget(document.createElement('input'))).toBe(true);
    expect(isEditableTarget(document.createElement('textarea'))).toBe(true);
  });

  it('treats elements inside a Monaco editor as editable (EditContext mode)', () => {
    const editor = document.createElement('div');
    editor.className = 'monaco-editor';
    const inner = document.createElement('div'); // not a textarea, not contenteditable
    editor.appendChild(inner);
    expect(isEditableTarget(inner)).toBe(true);
    expect(isEditableTarget(editor)).toBe(true);
  });

  it('is false for ordinary elements and non-elements', () => {
    expect(isEditableTarget(document.createElement('div'))).toBe(false);
    expect(isEditableTarget(document.createElement('button'))).toBe(false);
    expect(isEditableTarget(null)).toBe(false);
  });
});
