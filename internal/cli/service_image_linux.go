//go:build linux

package cli

// platformImageOverride returns "" on Linux: the embedded quadlet image is
// already correct for Linux (postgis/postgis works fine on libpod with crun).
func platformImageOverride(_, _ string) string {
	return ""
}
