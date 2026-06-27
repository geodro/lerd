package cli

import (
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// runCleanupAutoToggle persists the auto_cleanup flag so the watcher and event
// hooks read it back. Default is on; off then on must round-trip through config.
func TestCleanupAutoToggle_PersistsFlag(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AutoCleanupEnabled() {
		t.Fatal("auto cleanup should start enabled by default")
	}

	if err := runCleanupAutoToggle(false); err != nil {
		t.Fatalf("toggle off: %v", err)
	}
	if cfg, _ := config.LoadGlobal(); cfg.AutoCleanupEnabled() {
		t.Error("expected disabled after toggle off")
	}

	if err := runCleanupAutoToggle(true); err != nil {
		t.Fatalf("toggle on: %v", err)
	}
	if cfg, _ := config.LoadGlobal(); !cfg.AutoCleanupEnabled() {
		t.Error("expected enabled after toggle on")
	}
}
