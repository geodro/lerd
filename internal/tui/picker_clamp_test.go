package tui

import "testing"

func TestPhpDisabledMask(t *testing.T) {
	versions := []string{"7.4", "8.1", "8.3", "8.4", "8.5"}

	t.Run("no range disables nothing", func(t *testing.T) {
		mask := phpDisabledMask(versions, "", "", "8.4")
		for i, d := range mask {
			if d {
				t.Errorf("%s disabled with no range", versions[i])
			}
		}
	})

	t.Run("disables out-of-range versions", func(t *testing.T) {
		// Laravel 10 range 8.1–8.3, site on 8.3.
		mask := phpDisabledMask(versions, "8.1", "8.3", "8.3")
		want := map[string]bool{"7.4": true, "8.4": true, "8.5": true}
		for i, v := range versions {
			if mask[i] != want[v] {
				t.Errorf("%s disabled=%v, want %v", v, mask[i], want[v])
			}
		}
	})

	t.Run("never disables the current version", func(t *testing.T) {
		// Legacy site pinned to 7.4 keeps it selectable even below the range.
		mask := phpDisabledMask(versions, "8.1", "8.3", "7.4")
		if mask[0] {
			t.Error("current version 7.4 should not be disabled")
		}
	})
}

func TestFirstEnabledFrom(t *testing.T) {
	cases := []struct {
		name     string
		start    int
		disabled []bool
		want     int
	}{
		{"start enabled", 1, []bool{false, false, true}, 1},
		{"skip forward", 0, []bool{true, true, false}, 2},
		{"wrap to top", 2, []bool{false, true, true}, 0},
		{"all disabled returns start", 1, []bool{true, true, true}, 1},
		{"empty returns start", 0, nil, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := firstEnabledFrom(c.start, c.disabled); got != c.want {
				t.Errorf("firstEnabledFrom(%d, %v) = %d, want %d", c.start, c.disabled, got, c.want)
			}
		})
	}
}

// Navigation must land only on enabled entries, skipping disabled ones.
func TestPickerNavSkipsDisabled(t *testing.T) {
	m := &Model{
		activeTab:      tabSites,
		focus:          paneDetail,
		pickerKind:     kindPHP,
		pickerOptions:  []string{"7.4", "8.1", "8.3", "8.4"},
		pickerDisabled: []bool{false, true, true, false},
		pickerCursor:   0,
	}
	m.moveCursor(1)
	if m.pickerCursor != 3 {
		t.Errorf("after down from 0, cursor = %d, want 3 (8.1 and 8.3 disabled)", m.pickerCursor)
	}
	m.moveCursor(-1)
	if m.pickerCursor != 0 {
		t.Errorf("after up from 3, cursor = %d, want 0", m.pickerCursor)
	}
}
