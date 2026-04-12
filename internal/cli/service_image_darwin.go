//go:build darwin

package cli

// platformImageOverride substitutes images that don't work on macOS Podman
// Machine. The official postgis/postgis image is amd64-only and was deprecated
// upstream; imresamu/postgis is the actively maintained multi-arch fork that
// runs natively on Apple Silicon under Podman Machine.
func platformImageOverride(name string) string {
	switch name {
	case "postgres":
		return "docker.io/imresamu/postgis:16-3.5-alpine"
	}
	return ""
}
