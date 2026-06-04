//go:build darwin

package podman

import "strings"

// imageLacksArm64 reports whether image is a known upstream tag that ships no
// arm64 manifest, so Apple Silicon must pull and run it as linux/amd64 under
// qemu/Rosetta. Matched by substring because the pull path only has the image.
//
//   - postgis/postgis: no arm64 manifest for any tag.
//   - mysql:5.7: amd64-only (8.4 and 9.7 do publish arm64, so match the tag).
//
// User-pinned arm64 forks (e.g. imresamu/postgis) lack the substring and pass.
func imageLacksArm64(image string) bool {
	return strings.Contains(image, "postgis/postgis") ||
		strings.Contains(image, "mysql:5.7")
}

// PlatformPodmanArgs returns one extra `podman run` arg to splice into a
// service unit on macOS, or "" when no platform tweak is needed. Hooked from
// WriteQuadletDiff so cli, UI, MCP, and install all emit identical units. It
// keys off the image alone (imageLacksArm64), so the quadlet's --platform
// matches what PlatformPullArgs pins at pull time and the two never disagree,
// regardless of the service's name.
func PlatformPodmanArgs(_, currentImage string) string {
	if imageLacksArm64(currentImage) {
		return "--platform=linux/amd64"
	}
	return ""
}

// PlatformPullArgs returns extra `podman pull` flags for image on this host, or
// nil. The pull path only knows the image (not the service name), so it keys
// off imageLacksArm64: those tags ship no arm64 manifest, so without --platform
// podman fails picking an arch from the manifest list (exit 125). Pinning amd64
// here matches what PlatformPodmanArgs writes into the quadlet, so pull and run
// agree.
func PlatformPullArgs(image string) []string {
	if imageLacksArm64(image) {
		return []string{"--platform=linux/amd64"}
	}
	return nil
}
