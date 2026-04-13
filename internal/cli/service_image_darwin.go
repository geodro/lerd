//go:build darwin

package cli

import "strings"

// platformImageOverride substitutes images that don't work on macOS Podman
// Machine. The official postgis/postgis image is amd64-only and was deprecated
// upstream; imresamu/postgis is the actively maintained multi-arch fork that
// runs natively on Apple Silicon under Podman Machine.
//
// currentImage is the image that would otherwise be used (from global config or
// the embedded quadlet). The override is only applied when currentImage is one
// of the known-bad images so that user-pinned custom images are left untouched.
func platformImageOverride(name, currentImage string) string {
	switch name {
	case "postgres":
		// postgis/postgis alpine tags have no ARM64 manifest; imresamu/postgis
		// is the maintained multi-arch fork. Only override when the resolved
		// image is from the upstream repo with the alpine suffix.
		if strings.Contains(currentImage, "postgis/postgis") && strings.Contains(currentImage, "alpine") {
			return "docker.io/imresamu/postgis:16-3.5-alpine"
		}
	}
	return ""
}
