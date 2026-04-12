package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestQuadletImage_found(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := filepath.Join(tmp, "containers", "systemd")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "lerd-nginx.container"), []byte("[Container]\nImage=docker.io/library/nginx:alpine\n"), 0644)

	got := quadletImage("lerd-nginx")
	if got != "docker.io/library/nginx:alpine" {
		t.Errorf("quadletImage = %q, want docker.io/library/nginx:alpine", got)
	}
}

func TestQuadletImage_missing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	got := quadletImage("lerd-nonexistent")
	if got != "" {
		t.Errorf("quadletImage = %q, want empty for missing unit", got)
	}
}

func TestQuadletImage_noImageLine(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := filepath.Join(tmp, "containers", "systemd")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "lerd-test.container"), []byte("[Container]\nContainerName=test\n"), 0644)

	got := quadletImage("lerd-test")
	if got != "" {
		t.Errorf("quadletImage = %q, want empty when no Image= line", got)
	}
}

func TestEnsurePodmanMachineRunning_linux(t *testing.T) {
	// On Linux this is a no-op — should not panic
	ensurePodmanMachineRunning()
}

func TestMigrateExecWorkerPlists_linux(t *testing.T) {
	// On Linux this is a no-op — should not panic
	migrateExecWorkerPlists()
}
