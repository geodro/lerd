//go:build darwin

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFakePodman drops an executable `podman` shim into dir that runs the given
// shell body, and returns dir so it can be prepended to PATH.
func writeFakePodman(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" + body
	if err := os.WriteFile(filepath.Join(dir, "podman"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake podman: %v", err)
	}
	return dir
}

func withPATH(t *testing.T, dir string) {
	t.Helper()
	orig := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", orig) })
	os.Setenv("PATH", dir+":"+orig)
}

// On new macOS (e.g. Tahoe) vfkit can crash on the first `machine start`, leaving
// a stale SSH port behind; a second start reassigns it and boots. The helper must
// retry once and report success without surfacing the first failure as fatal.
func TestStartPodmanMachineWithRetry_RecoversOnSecondAttempt(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "n")
	dir := writeFakePodman(t, `
if [ "$1" = "machine" ] && [ "$2" = "start" ]; then
  n=$(cat "`+counter+`" 2>/dev/null || echo 0); n=$((n+1)); echo "$n" > "`+counter+`"
  [ "$n" -eq 1 ] && { echo "vfkit exited unexpectedly with exit code 1" >&2; exit 125; }
  exit 0
fi
exit 0
`)
	withPATH(t, dir)

	if err := startPodmanMachineWithRetry(); err != nil {
		t.Fatalf("expected recovery on retry, got %v", err)
	}
	if b, _ := os.ReadFile(counter); string(b) != "2\n" {
		t.Fatalf("expected exactly 2 start attempts, got %q", b)
	}
}

// When the VM never boots, the helper returns an error so install/start halt
// instead of cascading into a wall of confusing "exit status 125" failures.
func TestStartPodmanMachineWithRetry_FailsAfterRetry(t *testing.T) {
	dir := writeFakePodman(t, `
if [ "$1" = "machine" ] && [ "$2" = "start" ]; then
  echo "vfkit exited unexpectedly with exit code 1" >&2; exit 125
fi
exit 0
`)
	withPATH(t, dir)

	if err := startPodmanMachineWithRetry(); err == nil {
		t.Fatal("expected an error when the VM never boots, got nil")
	}
}
