package podman

import (
	"testing"
)

func TestWriteContainerUnitFnDefault(t *testing.T) {
	if WriteContainerUnitFn == nil {
		t.Fatal("WriteContainerUnitFn should default to WriteQuadlet")
	}
}

func TestDaemonReloadFnDefault(t *testing.T) {
	if DaemonReloadFn == nil {
		t.Fatal("DaemonReloadFn should default to DaemonReload")
	}
}

func TestSkipQuadletUpToDateCheckDefault(t *testing.T) {
	if SkipQuadletUpToDateCheck {
		t.Error("SkipQuadletUpToDateCheck should default to false")
	}
}
