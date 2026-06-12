package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/podman"
)

// removeContainerBun should clear every entry in the shared bun volume so a
// later install starts clean, while leaving the mount dir itself in place.
func TestRemoveContainerBun_ClearsVolume(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))

	dir := podman.BunVolumeDir()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "bun"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := removeContainerBun(&out); err != nil {
		t.Fatalf("removeContainerBun: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("bun volume dir should still exist: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("bun volume should be empty after remove, has %d entries", len(entries))
	}
	if !strings.Contains(out.String(), "Removed the in-container bun") {
		t.Errorf("unexpected output: %q", out.String())
	}
}

// On a volume that was never populated, remove is a no-op that says so rather
// than erroring, so reruns and never-installed states stay friendly.
func TestRemoveContainerBun_NothingInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))

	var out bytes.Buffer
	if err := removeContainerBun(&out); err != nil {
		t.Fatalf("removeContainerBun: %v", err)
	}
	if !strings.Contains(out.String(), "nothing to remove") {
		t.Errorf("expected nothing-to-remove message, got: %q", out.String())
	}
}
