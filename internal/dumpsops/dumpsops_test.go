package dumpsops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func withTempXDG(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	if err := os.MkdirAll(config.QuadletDir(), 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestApply_NoChangeWhenAlreadyOff(t *testing.T) {
	withTempXDG(t)
	res, err := Apply(false)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !res.NoChange || res.Enabled {
		t.Errorf("Result = %+v, want NoChange && !Enabled", res)
	}
}

func TestApply_OnWritesSentinelAndAssets(t *testing.T) {
	withTempXDG(t)
	res, err := Apply(true)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !res.Enabled || res.NoChange {
		t.Errorf("Result = %+v, want Enabled and !NoChange", res)
	}
	if _, err := os.Stat(config.DumpsBridgeFile()); err != nil {
		t.Errorf("bridge missing: %v", err)
	}
	if _, err := os.Stat(config.DumpsIniFile()); err != nil {
		t.Errorf("ini missing: %v", err)
	}
	if _, err := os.Stat(config.DumpsEnabledFlagFile()); err != nil {
		t.Errorf("sentinel missing: %v", err)
	}
	cfg, _ := config.LoadGlobal()
	if !cfg.IsDumpsEnabled() {
		t.Errorf("config not persisted as enabled")
	}
}

func TestApply_OffRemovesSentinelButKeepsAssets(t *testing.T) {
	withTempXDG(t)
	if _, err := Apply(true); err != nil {
		t.Fatal(err)
	}
	res, err := Apply(false)
	if err != nil {
		t.Fatalf("Apply off: %v", err)
	}
	if res.Enabled {
		t.Errorf("Result.Enabled = true, want false")
	}
	if _, err := os.Stat(config.DumpsEnabledFlagFile()); !os.IsNotExist(err) {
		t.Errorf("sentinel still present: %v", err)
	}
	// Assets stay on disk so the always-mounted FPM volumes keep working
	// without forcing a container restart.
	if _, err := os.Stat(config.DumpsBridgeFile()); err != nil {
		t.Errorf("bridge removed on disable, want preserved: %v", err)
	}
	if _, err := os.Stat(config.DumpsIniFile()); err != nil {
		t.Errorf("ini removed on disable, want preserved: %v", err)
	}
}

func TestApply_IsIdempotent(t *testing.T) {
	withTempXDG(t)
	if _, err := Apply(true); err != nil {
		t.Fatal(err)
	}
	res, err := Apply(true)
	if err != nil {
		t.Fatalf("second Apply(true): %v", err)
	}
	if !res.NoChange {
		t.Errorf("second Apply(true) = %+v, want NoChange", res)
	}
}
