//go:build !linux

package systemd

import "context"

// SubscribeLerdUnitStateChanges is a no-op on non-Linux platforms: macOS
// has no equivalent subscription model via launchctl. Callers continue to
// rely on periodic polling for state updates on that platform.
func SubscribeLerdUnitStateChanges(_ context.Context, _ func(string)) error {
	return nil
}
