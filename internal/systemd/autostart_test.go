package systemd

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestAutostartUserUnits verifies that AutostartUserUnits returns the
// lerd-ui / lerd-watcher / lerd-tray baseline plus every per-site
// lerd-*.service in the systemd/user/ directory, deduplicated and
// sorted, and that it ignores non-lerd units.
func TestAutostartUserUnits(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	systemdDir := filepath.Join(tmp, "systemd", "user")
	if err := os.MkdirAll(systemdDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Per-site / per-worker units. lerd-ui is duplicated to verify dedup.
	for _, name := range []string{
		"lerd-ui.service",
		"lerd-watcher.service",
		"lerd-tray.service",
		"lerd-queue-myapp.service",
		"lerd-schedule-myapp.service",
		"lerd-horizon-myapp.service",
		"lerd-reverb-myapp.service",
		"lerd-stripe-myapp.service",
	} {
		if err := os.WriteFile(filepath.Join(systemdDir, name), []byte("[Service]\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// A non-lerd unit that must be ignored.
	if err := os.WriteFile(filepath.Join(systemdDir, "other.service"), []byte("[Service]\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got := AutostartUserUnits()
	want := []string{
		"lerd-horizon-myapp.service",
		"lerd-queue-myapp.service",
		"lerd-reverb-myapp.service",
		"lerd-schedule-myapp.service",
		"lerd-stripe-myapp.service",
		"lerd-tray.service",
		"lerd-ui.service",
		"lerd-watcher.service",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AutostartUserUnits() mismatch\ngot:  %v\nwant: %v", got, want)
	}
}

// TestAutostartUserUnitsBaseline verifies that lerd-ui, lerd-watcher
// and lerd-tray are always included even when no files exist on disk.
// Without this, a fresh install (no per-site units yet) would have
// nothing to enable when the user toggles autostart back on.
func TestAutostartUserUnitsBaseline(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	got := AutostartUserUnits()
	want := []string{"lerd-tray.service", "lerd-ui.service", "lerd-watcher.service"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AutostartUserUnits() baseline mismatch\ngot:  %v\nwant: %v", got, want)
	}
}

// TestIsAutostartEnabledDefault verifies the safe default — when no
// config exists yet, autostart is treated as enabled, matching the
// historical behaviour of every install before this flag existed.
func TestIsAutostartEnabledDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "share"))

	if !IsAutostartEnabled() {
		t.Error("IsAutostartEnabled() should default to true when no config exists")
	}
}
