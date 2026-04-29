package serviceops

import (
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// TestRollback_RequiresPreviousImage covers the contract that rollback fails
// loudly when there's nothing to roll back to.
func TestRollback_RequiresPreviousImage(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	err := RollbackService("mysql", func(PhaseEvent) {})
	if err == nil {
		t.Fatal("RollbackService must error when no previous image is recorded")
	}
}

// TestPersistImageChoice_RecordsPrevious covers the recorder side: each
// update must capture the old image so rollback has a target.
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

	if err := persistRecordOnly("redis", "docker.io/library/redis:7.4.9-alpine"); err != nil {
		t.Fatalf("persistRecordOnly: %v", err)
	}
	cfg3, _ := config.LoadGlobal()
	got = cfg3.Services["redis"]
	if got.PreviousImage != "docker.io/library/redis:7.4.8-alpine" {
		t.Errorf("PreviousImage after second update = %q, want 7.4.8-alpine", got.PreviousImage)
	}
}

// TestRollback_RefusesAfterMigrate guards against running the previous binary
// against a fresh (post-migrate) data dir.
func TestRollback_RefusesAfterMigrate(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	cfg.Services["mysql"] = config.ServiceConfig{
		Enabled:          true,
		Image:            "docker.io/library/mysql:8.4",
		PreviousImage:    "docker.io/library/mysql:8.0",
		LastOp:           "migrate",
		PreMigrateBackup: "/tmp/mysql.pre-migrate-20260428-1200",
		Port:             3306,
	}
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	err = RollbackService("mysql", func(PhaseEvent) {})
	if err == nil {
		t.Fatal("RollbackService must refuse when last op was a migrate")
	}
	if !strings.Contains(err.Error(), "migrate") {
		t.Errorf("rollback error should mention migrate, got %q", err)
	}
}

// TestCheckUpdateAvailable_CanRollback flips false when the last op was a
// migrate so the UI can hide the rollback button.
func TestCheckUpdateAvailable_CanRollback(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("LERD_REGISTRY_CACHE_DIR", tmp+"/cache")
	// Empty cache + no network access in tests means the registry probe
	// returns nothing useful, but CanRollback is computed before that.

	cfg, _ := config.LoadGlobal()
	cfg.Services["redis"] = config.ServiceConfig{
		Enabled:       true,
		Image:         "docker.io/library/redis:7.4.9-alpine",
		PreviousImage: "docker.io/library/redis:7.4.8-alpine",
		LastOp:        "update",
	}
	_ = config.SaveGlobal(cfg)

	avail, err := CheckUpdateAvailable("redis")
	if err != nil {
		t.Fatalf("CheckUpdateAvailable: %v", err)
	}
	if !avail.CanRollback {
		t.Errorf("CanRollback = false, want true after a plain update")
	}

	cfg.Services["redis"] = config.ServiceConfig{
		Enabled:       true,
		Image:         "docker.io/library/redis:7.4.9-alpine",
		PreviousImage: "docker.io/library/redis:7.4.8-alpine",
		LastOp:        "migrate",
	}
	_ = config.SaveGlobal(cfg)
	// Mirror what the apply paths do: out-of-band config mutations must drop
	// the cached result so the next read recomputes against fresh state.
	invalidateUpdateAvailability("redis")

	avail, err = CheckUpdateAvailable("redis")
	if err != nil {
		t.Fatalf("CheckUpdateAvailable (migrate state): %v", err)
	}
	if avail.CanRollback {
		t.Errorf("CanRollback = true after migrate, want false")
	}
}

// TestEnforceMajorUpgradeGate refuses cross-major when the preset's
// allow_major_upgrade is false. mysql is the canonical case.
func TestEnforceMajorUpgradeGate(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	cfg, _ := config.LoadGlobal()
	cfg.Services["mysql"] = config.ServiceConfig{
		Enabled: true,
		Image:   "docker.io/library/mysql:8.4",
	}
	_ = config.SaveGlobal(cfg)

	if err := enforceMajorUpgradeGate("mysql", "docker.io/library/mysql:9.0"); err == nil {
		t.Fatal("enforceMajorUpgradeGate must refuse 8.4 → 9.0 when allow_major_upgrade=false")
	}
	if err := enforceMajorUpgradeGate("mysql", "docker.io/library/mysql:8.4.10"); err != nil {
		t.Errorf("8.4 → 8.4.10 should be allowed, got %v", err)
	}
}

// TestLeadingMajor parses the leading numeric prefix of common tag shapes.
func TestLeadingMajor(t *testing.T) {
	cases := map[string]int{
		"8.4":             8,
		"v8.4":            8,
		"8.4.10":          8,
		"7-alpine":        7,
		"latest":          -1,
		"":                -1,
		"16-3.5-bookworm": 16,
	}
	for in, want := range cases {
		if got := leadingMajor(in); got != want {
			t.Errorf("leadingMajor(%q) = %d, want %d", in, got, want)
		}
	}
}

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
	svcCfg.LastOp = "update"
	cfg.Services[name] = svcCfg
	return config.SaveGlobal(cfg)
}
