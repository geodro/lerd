//go:build !darwin

package php

// listInstalledFromServiceDir is a no-op on Linux; PHP versions are discovered
// via the quadlet directory glob in ListInstalled.
func listInstalledFromServiceDir() []string { return nil }
