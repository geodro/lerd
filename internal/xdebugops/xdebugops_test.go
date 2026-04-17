package xdebugops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// setupConfigHome points config.* path helpers at a temp tree and neutralises
// PATH so podman.RestartUnit inside Apply fails fast instead of actually
// restarting a unit on the dev machine running the tests.
func setupConfigHome(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("PATH", filepath.Join(tmp, "no-bin"))

	// WriteFPMQuadlet shells out through a hook; stub it so the test doesn't
	// need the full container environment. DaemonReload is stubbed for the
	// same reason.
	prevWrite := podman.WriteContainerUnitFn
	prevReload := podman.DaemonReloadFn
	podman.WriteContainerUnitFn = func(name, content string) error { return nil }
	podman.DaemonReloadFn = func() error { return nil }
	t.Cleanup(func() {
		podman.WriteContainerUnitFn = prevWrite
		podman.DaemonReloadFn = prevReload
	})
}

func TestApply_EnablesWithDefaultMode(t *testing.T) {
	setupConfigHome(t)

	res, err := Apply("8.4", "debug")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !res.Enabled || res.Mode != "debug" {
		t.Errorf("unexpected result: %+v", res)
	}

	cfg, _ := config.LoadGlobal()
	if cfg.GetXdebugMode("8.4") != "debug" {
		t.Errorf("config not persisted: mode=%q", cfg.GetXdebugMode("8.4"))
	}

	body, _ := os.ReadFile(config.PHPConfFile("8.4"))
	if !strings.Contains(string(body), "xdebug.mode=debug") {
		t.Errorf("ini missing xdebug.mode=debug:\n%s", body)
	}
}

func TestApply_EnablesWithCoverageMode(t *testing.T) {
	setupConfigHome(t)

	res, err := Apply("8.4", "coverage")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Mode != "coverage" {
		t.Errorf("Mode = %q, want %q", res.Mode, "coverage")
	}
	body, _ := os.ReadFile(config.PHPConfFile("8.4"))
	if !strings.Contains(string(body), "xdebug.mode=coverage") {
		t.Errorf("ini missing xdebug.mode=coverage:\n%s", body)
	}
}

func TestApply_DisableWritesOff(t *testing.T) {
	setupConfigHome(t)

	if _, err := Apply("8.4", "debug"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	res, err := Apply("8.4", "")
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if res.Enabled || res.Mode != "" {
		t.Errorf("expected disabled, got %+v", res)
	}
	body, _ := os.ReadFile(config.PHPConfFile("8.4"))
	if !strings.Contains(string(body), "xdebug.mode=off") {
		t.Errorf("ini missing xdebug.mode=off:\n%s", body)
	}
}

func TestApply_NoChangeOnIdempotentCall(t *testing.T) {
	setupConfigHome(t)

	if _, err := Apply("8.4", "coverage"); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	res, err := Apply("8.4", "coverage")
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if !res.NoChange {
		t.Errorf("second Apply should be a no-op, got %+v", res)
	}
	if res.Restarted {
		t.Errorf("NoChange result must not claim a restart")
	}
}

func TestApply_RejectsInvalidMode(t *testing.T) {
	setupConfigHome(t)
	if _, err := Apply("8.4", "nonsense"); err == nil {
		t.Error("expected invalid-mode error, got nil")
	}
}

func TestApply_RestartErrIsNonFatal(t *testing.T) {
	// PATH is empty so podman.RestartUnit will fail. Apply must still
	// persist config and ini, returning RestartErr instead of aborting;
	// otherwise a harmless restart hiccup loses the user's toggle.
	setupConfigHome(t)

	res, err := Apply("8.4", "debug")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.RestartErr == nil {
		t.Fatal("expected RestartErr when podman is unavailable")
	}
	if !res.Enabled || res.Mode != "debug" {
		t.Errorf("config/ini should still be applied: %+v", res)
	}
	cfg, _ := config.LoadGlobal()
	if cfg.GetXdebugMode("8.4") != "debug" {
		t.Error("config not saved despite RestartErr")
	}
}

func TestFPMUnit(t *testing.T) {
	if got := FPMUnit("8.4"); got != "lerd-php84-fpm" {
		t.Errorf("FPMUnit(8.4) = %q, want lerd-php84-fpm", got)
	}
	if got := FPMUnit("8.10"); got != "lerd-php810-fpm" {
		t.Errorf("FPMUnit(8.10) = %q, want lerd-php810-fpm", got)
	}
}
