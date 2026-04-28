//go:build !linux

package systemd

import "errors"

// errNoDBus is returned for every DBus-routed call on non-Linux platforms.
// Callers that care (the Linux user service manager) never reach these stubs
// because they are Linux-only; the stubs exist so the systemd package still
// compiles on macOS where cross-platform CLI code imports it.
var errNoDBus = errors.New("systemd DBus not available on this platform")

func DBusDaemonReload() error         { return errNoDBus }
func DBusStartUnit(string) error      { return errNoDBus }
func DBusStopUnit(string) error       { return errNoDBus }
func DBusRestartUnit(string) error    { return errNoDBus }
func DBusResetFailed(string) error    { return errNoDBus }
func DBusEnableService(string) error  { return errNoDBus }
func DBusDisableService(string) error { return errNoDBus }
func DBusActiveState(string) string   { return "" }
func DBusIsEnabled(string) bool       { return false }

// NotifyReady is a no-op on non-Linux platforms (no sd_notify).
func NotifyReady() {}

// NotifyStopping is a no-op on non-Linux platforms (no sd_notify).
func NotifyStopping() {}
