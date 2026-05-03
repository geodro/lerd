//go:build darwin

package podman

import "strings"

// PlatformPodmanArgs returns one extra `podman run` arg to splice into a
// service unit on macOS, or "" when no platform tweak is needed. Hooked from
// WriteQuadletDiff so cli, UI, MCP, and install all emit identical units.
//
// postgis/postgis has no arm64 manifest for any tag, so on Apple Silicon we
// pin --platform=linux/amd64 and let Podman Machine run it under qemu/Rosetta.
// User-pinned arm64 forks (e.g. imresamu/postgis) lack the substring and pass.
func PlatformPodmanArgs(serviceName, currentImage string) string {
	if serviceName == "postgres" && strings.Contains(currentImage, "postgis/postgis") {
		return "--platform=linux/amd64"
	}
	return ""
}
