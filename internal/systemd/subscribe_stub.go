//go:build !linux

package systemd

import "context"

// UnitStateWatcher is a no-op stub on non-Linux platforms: macOS has no
// equivalent subscription model via launchctl. Callers continue to rely on
// periodic polling for state updates on that platform.
type UnitStateWatcher struct{}

func NewUnitStateWatcher(_ func(string)) *UnitStateWatcher { return &UnitStateWatcher{} }

func (w *UnitStateWatcher) Start() error { return nil }
func (w *UnitStateWatcher) Stop()        {}

// SubscribeLerdUnitStateChanges keeps the previous one-shot subscription
// helper for non-UI callers; on non-Linux it's a no-op.
func SubscribeLerdUnitStateChanges(_ context.Context, _ func(string)) error {
	return nil
}
