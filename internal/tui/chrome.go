// Package-level UI helpers: small render primitives that don't fit cleanly
// into any one feature module. Today this holds the key-chip pill renderer
// (used in modal footers and toast actions) and the spinner glyph picker
// (used in the status area when an action is in flight).
package tui

import (
	"strings"
	"time"
)

// keyChip renders one keybinding as a styled pill followed by a dim label,
// e.g. ` y `bold-accent` ` `confirm`-dim. Used in modal footers so the user
// reads the shortcut as an inline button instead of as prose. Caller is
// responsible for adding spacing between chips.
func keyChip(key, label string) string {
	return keyChipStyle.Render(" "+key+" ") + " " + keyChipLabelStyle.Render(label)
}

// renderKeyChips joins multiple key-chip pairs with two spaces between
// them. Pass alternating key, label, key, label, …; an odd-count input is
// silently truncated to the largest valid pair count.
func renderKeyChips(pairs ...string) string {
	if len(pairs)%2 != 0 {
		pairs = pairs[:len(pairs)-1]
	}
	parts := make([]string, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		parts = append(parts, keyChip(pairs[i], pairs[i+1]))
	}
	return strings.Join(parts, "  ")
}

// spinnerFrame returns the spinner glyph for the current moment. Wall-clock
// driven so every redraw cycles to the next frame without needing a
// dedicated ticker. ~100ms per frame keeps it lively without being noisy.
func spinnerFrame() string {
	idx := (time.Now().UnixMilli() / 100) % int64(len(spinnerFrames))
	return spinnerFrames[idx]
}

// renderSpinnerStatus formats an in-flight action line for the status area:
// spinner glyph + verb-phrase. Pass the same string the action stored in
// setStatus and the user sees `⠹ restarting redis…` instead of static text.
func renderSpinnerStatus(verb string) string {
	return accentStyle.Render(spinnerFrame()) + "  " + verb
}
