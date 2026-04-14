package ui

import (
	"os"
	"strings"
	"testing"
)

func TestGraphicalEnvPreservesBaseEnvAndPatchesDisplay(t *testing.T) {
	t.Setenv("LERD_TEST_SENTINEL", "abc123")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")

	runtimeDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	if err := os.WriteFile(runtimeDir+"/wayland-7", []byte{}, 0o600); err != nil {
		t.Fatalf("seed wayland socket: %v", err)
	}

	env := graphicalEnv()

	var sawSentinel, sawWayland, sawRuntimeDir bool
	var waylandVal string
	for _, kv := range env {
		switch {
		case kv == "LERD_TEST_SENTINEL=abc123":
			sawSentinel = true
		case strings.HasPrefix(kv, "WAYLAND_DISPLAY="):
			sawWayland = true
			waylandVal = strings.TrimPrefix(kv, "WAYLAND_DISPLAY=")
		case kv == "XDG_RUNTIME_DIR="+runtimeDir:
			sawRuntimeDir = true
		}
	}

	if !sawSentinel {
		t.Error("graphicalEnv dropped base environment entry")
	}
	if !sawRuntimeDir {
		t.Error("graphicalEnv did not preserve XDG_RUNTIME_DIR")
	}
	if !sawWayland {
		t.Error("graphicalEnv did not probe WAYLAND_DISPLAY from XDG_RUNTIME_DIR")
	}
	if sawWayland && waylandVal != "wayland-7" {
		t.Errorf("WAYLAND_DISPLAY = %q, want wayland-7", waylandVal)
	}
}

func TestGraphicalEnvDoesNotDuplicateKeys(t *testing.T) {
	t.Setenv("XDG_SESSION_TYPE", "wayland")
	env := graphicalEnv()
	count := 0
	for _, kv := range env {
		if strings.HasPrefix(kv, "XDG_SESSION_TYPE=") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("XDG_SESSION_TYPE appears %d times, want 1", count)
	}
}
