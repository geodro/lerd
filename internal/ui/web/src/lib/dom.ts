// isEditableTarget reports whether an event target is a place the user is
// typing, so global single-key shortcuts (e.g. `/` to open the command
// palette) can bow out instead of hijacking the keystroke.
//
// It matches the obvious form fields and contenteditable, plus the Monaco
// editor: Monaco's editable surface is a hidden <textarea> in classic mode
// but a plain <div> under the EditContext input model, so a tag check alone
// misses it, so we also treat anything inside a `.monaco-editor` as editable.
export function isEditableTarget(t: EventTarget | null): boolean {
  if (!(t instanceof HTMLElement)) return false;
  const tag = t.tagName.toLowerCase();
  if (tag === 'input' || tag === 'textarea' || t.isContentEditable) return true;
  return !!t.closest('.monaco-editor');
}
