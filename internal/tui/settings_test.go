package tui

import (
	"runtime"
	"testing"
)

// The worker-mode row should only be present on macOS so the Linux
// settings overlay doesn't advertise a setting that has no effect.
func TestSettingsRows_WorkerModeVisibilityMatchesPlatform(t *testing.T) {
	m := NewModel("test")
	rows := m.settingsRows()

	var found bool
	for _, r := range rows {
		if r.kind == settingsWorkerMode {
			found = true
			break
		}
	}

	wantPresent := runtime.GOOS == "darwin"
	if found != wantPresent {
		t.Errorf("worker-mode row present=%v on %s, want present=%v",
			found, runtime.GOOS, wantPresent)
	}
}
