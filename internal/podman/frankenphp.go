package podman

import (
	"fmt"
	"sort"
	"strings"

	"github.com/geodro/lerd/internal/config"
)

// FrankenPHPContainerName returns the Podman container name for a site's
// FrankenPHP container, e.g. "lerd-fp-myapp".
func FrankenPHPContainerName(siteName string) string {
	return "lerd-fp-" + siteName
}

// FrankenPHPImage returns the official dunglas/frankenphp image tag for the
// requested PHP version. Versions without a published frankenphp image fall
// back to the latest one lerd knows about.
func FrankenPHPImage(phpVersion string) string {
	if !config.IsFrankenPHPVersion(phpVersion) {
		phpVersion = config.LatestFrankenPHPVersion()
	}
	return "docker.io/dunglas/frankenphp:php" + phpVersion + "-alpine"
}

// FrankenPHPPort is the port FrankenPHP listens on inside the container. Kept
// fixed so the nginx proxy target and framework entrypoints stay in sync.
const FrankenPHPPort = 8000

// GenerateFrankenPHPQuadlet builds a quadlet .container file for a per-site
// FrankenPHP container. The container mounts the project at its host path,
// joins the lerd network, and runs the framework's declared entrypoint. Any
// env map entries are written as Environment= lines.
func GenerateFrankenPHPQuadlet(siteName, projectPath, phpVersion string, entrypoint []string, env map[string]string) string {
	containerName := FrankenPHPContainerName(siteName)
	image := FrankenPHPImage(phpVersion)

	var b strings.Builder
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=Lerd FrankenPHP container (%s)\n", siteName)
	b.WriteString("After=network.target\n")

	b.WriteString("\n[Container]\n")
	fmt.Fprintf(&b, "Image=%s\n", image)
	fmt.Fprintf(&b, "ContainerName=%s\n", containerName)
	b.WriteString("Network=lerd\n")
	fmt.Fprintf(&b, "Volume=%s:/etc/hosts:ro,z\n", config.ContainerHostsFile())
	fmt.Fprintf(&b, "Volume=%s:%s:rw\n", projectPath, projectPath)
	fmt.Fprintf(&b, "PodmanArgs=--security-opt=label=disable --workdir=%s\n", projectPath)
	for _, k := range sortedKeys(env) {
		// systemd Environment= splits on whitespace unless the whole
		// KEY=value pair is quoted, so always wrap in double quotes.
		v := strings.ReplaceAll(env[k], `"`, `\"`)
		fmt.Fprintf(&b, "Environment=\"%s=%s\"\n", k, v)
	}
	if len(entrypoint) > 0 {
		fmt.Fprintf(&b, "Exec=%s\n", shellJoin(entrypoint))
	}

	b.WriteString("\n[Service]\n")
	b.WriteString("Restart=always\n")

	b.WriteString("\n[Install]\n")
	b.WriteString("WantedBy=default.target\n")

	return b.String()
}

// WriteFrankenPHPQuadlet writes the quadlet for a FrankenPHP site.
func WriteFrankenPHPQuadlet(siteName, projectPath, phpVersion string, entrypoint []string, env map[string]string) error {
	_, err := WriteFrankenPHPQuadletDiff(siteName, projectPath, phpVersion, entrypoint, env)
	return err
}

// WriteFrankenPHPQuadletDiff writes the quadlet and returns whether the content
// changed on disk so callers can decide whether to restart the running
// container.
func WriteFrankenPHPQuadletDiff(siteName, projectPath, phpVersion string, entrypoint []string, env map[string]string) (bool, error) {
	content := GenerateFrankenPHPQuadlet(siteName, projectPath, phpVersion, entrypoint, env)
	return WriteQuadletDiff(FrankenPHPContainerName(siteName), content)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// RemoveFrankenPHPQuadlet removes the unit file for a FrankenPHP site.
// RemoveQuadlet drops the launchd plist on macOS too via RemoveContainerUnitFn.
func RemoveFrankenPHPQuadlet(siteName string) error {
	return RemoveQuadlet(FrankenPHPContainerName(siteName))
}

// shellJoin quotes each argument for embedding in a quadlet Exec= line.
// Quadlet Exec values are passed through podman's argv parser which already
// handles single-word args; anything with whitespace needs quoting.
func shellJoin(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " \t\"'\\") {
			out[i] = `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
		} else {
			out[i] = a
		}
	}
	return strings.Join(out, " ")
}
