//go:build !darwin

package cli

import (
	"errors"
	"testing"
)

// On non-darwin platforms the overlay heal does not apply (no Podman Machine
// VM), so reportOverlayHealOutcome must never claim the error. If it did,
// runStart would stop early and skip the worker, DNS, and tray steps with no
// guidance printed, even though the matcher can match a Linux container-storage
// path. Guard that the gate stays open here.
func TestReportOverlayHealOutcomeIsNoopOnLinux(t *testing.T) {
	linuxOverlayErr := errors.New(`Error: getting graph driver info "abc": readlink /home/user/.local/share/containers/storage/overlay: invalid argument`)
	if reportOverlayHealOutcome(linuxOverlayErr) {
		t.Fatal("reportOverlayHealOutcome claimed a Linux overlay error; runStart would stop early without guidance")
	}
	if healOverlayCorruptionIfNeeded(linuxOverlayErr) {
		t.Fatal("healOverlayCorruptionIfNeeded ran on a non-darwin platform")
	}
}
