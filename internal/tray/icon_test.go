//go:build !nogui

package tray

import (
	"bytes"
	"testing"
)

func TestPickColorIcon(t *testing.T) {
	tests := []struct {
		name     string
		running  bool
		light    bool
		wantKind iconKind
		wantIcon []byte
	}{
		{"stopped dark panel", false, false, iconKindStopped, iconPNG},
		{"stopped light panel", false, true, iconKindStopped, iconPNG},
		{"running dark panel", true, false, iconKindRunningDark, iconWhitePNG},
		{"running light panel", true, true, iconKindRunningLight, iconDarkPNG},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, icon := pickColorIcon(tc.running, tc.light)
			if kind != tc.wantKind {
				t.Errorf("kind = %d, want %d", kind, tc.wantKind)
			}
			if !bytes.Equal(icon, tc.wantIcon) {
				t.Errorf("icon mismatch for %s", tc.name)
			}
		})
	}
}

// The dark running icon must be a distinct asset from the white one, otherwise
// the light-panel fix is a no-op.
func TestRunningIconsDiffer(t *testing.T) {
	if len(iconDarkPNG) == 0 {
		t.Fatal("iconDarkPNG is empty")
	}
	if bytes.Equal(iconDarkPNG, iconWhitePNG) {
		t.Error("dark and white running icons are identical")
	}
}

// setLight must flip the running icon between the white and dark variants
// without a running-state change, proving live theme switches take effect.
func TestIconStateLightSwitch(t *testing.T) {
	var applied [][]byte
	s := newIconState()
	s.apply = func(icon []byte) { applied = append(applied, icon) }

	s.setRunning(true)
	if !bytes.Equal(applied[len(applied)-1], iconWhitePNG) {
		t.Fatal("running on default dark panel should show the white icon")
	}
	s.setLight(true)
	if s.last != iconKindRunningLight || !bytes.Equal(applied[len(applied)-1], iconDarkPNG) {
		t.Error("light panel should swap the running icon to dark")
	}
	s.setLight(false)
	if s.last != iconKindRunningDark || !bytes.Equal(applied[len(applied)-1], iconWhitePNG) {
		t.Error("back to dark panel should restore the white icon")
	}
}

// Redundant updates must not re-issue SetIcon, so the systray isn't thrashed on
// every 5s poll tick.
func TestIconStateSkipsRedundant(t *testing.T) {
	var calls int
	s := newIconState()
	s.apply = func([]byte) { calls++ }

	s.setRunning(true)
	s.setRunning(true)
	s.setLight(false)
	if calls != 1 {
		t.Errorf("apply called %d times, want 1", calls)
	}
}
