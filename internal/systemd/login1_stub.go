//go:build !linux

package systemd

// SessionIsIdle always returns false on non-Linux platforms (no logind).
// Callers default to "treat as active" so watchers never starve on macOS.
func SessionIsIdle() bool { return false }

// SessionIsLocked always returns false on non-Linux platforms.
func SessionIsLocked() bool { return false }

// SessionIsIdleOrLocked always returns false on non-Linux platforms.
func SessionIsIdleOrLocked() bool { return false }

// Keep stub and linux variants in sync if more helpers are added.
