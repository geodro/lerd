package podman

import (
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// On macOS the host DB is reached over TCP (gvproxy's host.containers.internal),
// so there is no socket directory to bind-mount into FPM — hostDBSocketDirs must
// short-circuit to nil rather than inject a dead mount.
func TestHostDBSocketDirs_skippedOnMacOS(t *testing.T) {
	defer config.SetHostDBGOOSForTest("darwin")()
	if got := hostDBSocketDirs(); got != nil {
		t.Errorf("hostDBSocketDirs() on macOS = %v, want nil (host DB uses TCP, no socket mount)", got)
	}
}
