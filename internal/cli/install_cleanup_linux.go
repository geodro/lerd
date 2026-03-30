//go:build linux

package cli

// installCleanupScript is a no-op on Linux — uninstall is handled by `lerd uninstall`.
func installCleanupScript() {}
