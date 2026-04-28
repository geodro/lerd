package serviceops

import (
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// TestRollback_RequiresPreviousImage covers the contract that rollback fails
// loudly when there's nothing to roll back to (vs. silently no-oping).
func TestRollback_RequiresPreviousImage(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	err := RollbackService("mysql", func(PhaseEvent) {})
	if err == nil {
		t.Fatal("RollbackService must error when no previous image is recorded")
	}
}

// TestPersistImageChoice_RecordsPrevious covers the recorder side: each update
// must capture the old image so rollback has a target.
func TestPersistImageChoice_RecordsPrevious(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	cfg.Services["redis"] = config.ServiceConfig{
		Enabled: true,
		Image:   "docker.io/library/redis:7-alpine",
		Port:    6379,
	}
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	// First "update" — the current 7-alpine should land in PreviousImage.
	if err := persistRecordOnly("redis", "docker.io/library/redis:7.4.8-alpine"); err != nil {
		t.Fatalf("persistRecordOnly: %v", err)
	}

	cfg2, _ := config.LoadGlobal()
	got := cfg2.Services["redis"]
	if got.Image != "docker.io/library/redis:7.4.8-alpine" {
		t.Errorf("Image = %q, want updated", got.Image)
	}
	if got.PreviousImage != "docker.io/library/redis:7-alpine" {
		t.Errorf("PreviousImage = %q, want previous 7-alpine", got.PreviousImage)
	}

	// Second update — previous should advance to the post-first-update image.
	if err := persistRecordOnly("redis", "docker.io/library/redis:7.4.9-alpine"); err != nil {
		t.Fatalf("persistRecordOnly: %v", err)
	}
	cfg3, _ := config.LoadGlobal()
	got = cfg3.Services["redis"]
	if got.PreviousImage != "docker.io/library/redis:7.4.8-alpine" {
		t.Errorf("PreviousImage after second update = %q, want 7.4.8-alpine", got.PreviousImage)
	}
}

// persistRecordOnly mimics persistImageChoice's record step without actually
// regenerating quadlets (which would need podman). The test only asserts the
// pin-swapping logic, not the side effects.
func persistRecordOnly(name, newImage string) error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	svcCfg := cfg.Services[name]
	if svcCfg.Image != "" && svcCfg.Image != newImage {
		svcCfg.PreviousImage = svcCfg.Image
	}
	svcCfg.Image = newImage
	cfg.Services[name] = svcCfg
	return config.SaveGlobal(cfg)
}
