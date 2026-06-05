//go:build !darwin

package cli

// healOverlayCorruptionIfNeeded is a no-op on non-darwin platforms; the
// overlay-storage corruption it heals is specific to the macOS Podman Machine
// VM. Native rootless podman on Linux doesn't use a separate VM.
func healOverlayCorruptionIfNeeded(_ error) bool { return false }

// reportOverlayHealOutcome is a no-op on non-darwin platforms.
func reportOverlayHealOutcome(_ error) {}
