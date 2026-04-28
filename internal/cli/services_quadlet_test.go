package cli

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

func swapDaemonReload(t *testing.T) *int {
	t.Helper()
	count := new(int)
	orig := podman.DaemonReloadFn
	t.Cleanup(func() { podman.DaemonReloadFn = orig })
	podman.DaemonReloadFn = func() error {
		*count++
		return nil
	}
	return count
}

func setIsolatedXDG(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	t.Setenv("LERD_REGISTRY_CACHE_DIR", filepath.Join(tmp, "registry-cache"))
}

// pinMysqlImage writes a global config that pins mysql so the
// track_latest registry probe is skipped, keeping reload-counting
// tests deterministic regardless of network availability.
func pinMysqlImage(t *testing.T, image string) {
	t.Helper()
	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if cfg.Services == nil {
		cfg.Services = map[string]config.ServiceConfig{}
	}
	cfg.Services["mysql"] = config.ServiceConfig{Image: image}
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
}

func TestEnsureServiceQuadlet_reloadsOnlyWhenContentChanges(t *testing.T) {
	setIsolatedXDG(t)
	pinMysqlImage(t, "docker.io/library/mysql:8.4")
	count := swapDaemonReload(t)

	if err := ensureServiceQuadlet("mysql"); err != nil {
		t.Fatalf("first ensureServiceQuadlet: %v", err)
	}
	if *count != 1 {
		t.Errorf("first call should reload exactly once, got %d", *count)
	}

	if err := ensureServiceQuadlet("mysql"); err != nil {
		t.Fatalf("second ensureServiceQuadlet: %v", err)
	}
	if *count != 1 {
		t.Errorf("second call with unchanged content must not reload again, got %d total", *count)
	}
}

func TestEnsureServiceQuadlet_retriesAfterFailedReload(t *testing.T) {
	setIsolatedXDG(t)
	pinMysqlImage(t, "docker.io/library/mysql:8.4")

	failures := 0
	orig := podman.DaemonReloadFn
	t.Cleanup(func() { podman.DaemonReloadFn = orig })
	podman.DaemonReloadFn = func() error {
		failures++
		return errors.New("dbus down")
	}

	if err := ensureServiceQuadlet("mysql"); err == nil {
		t.Fatalf("expected reload error on first call")
	}
	if failures != 1 {
		t.Fatalf("expected 1 reload attempt, got %d", failures)
	}

	successes := 0
	podman.DaemonReloadFn = func() error {
		successes++
		return nil
	}

	if err := ensureServiceQuadlet("mysql"); err != nil {
		t.Fatalf("retry should succeed, got %v", err)
	}
	if successes != 1 {
		t.Errorf("retry must reload exactly once even though content is unchanged, got %d", successes)
	}

	if err := ensureServiceQuadlet("mysql"); err != nil {
		t.Fatalf("third call: %v", err)
	}
	if successes != 1 {
		t.Errorf("third call with unchanged content and pending cleared must not reload, got %d", successes)
	}
}

func TestEnsureServiceQuadlet_reloadsAgainWhenImageOverrideChanges(t *testing.T) {
	setIsolatedXDG(t)
	pinMysqlImage(t, "docker.io/library/mysql:8.0")
	count := swapDaemonReload(t)

	if err := ensureServiceQuadlet("mysql"); err != nil {
		t.Fatalf("first ensureServiceQuadlet: %v", err)
	}
	if *count != 1 {
		t.Fatalf("first call should reload, got %d", *count)
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	svc := cfg.Services["mysql"]
	svc.Image = "docker.io/library/mysql:8.4"
	cfg.Services["mysql"] = svc
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	if err := ensureServiceQuadlet("mysql"); err != nil {
		t.Fatalf("after image swap ensureServiceQuadlet: %v", err)
	}
	if *count != 2 {
		t.Errorf("changed image should trigger a second reload, got %d total", *count)
	}
}
