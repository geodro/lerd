package podman

import (
	"fmt"
	"strings"

	"github.com/geodro/lerd/internal/config"
)

// GenerateCustomContainerQuadlet builds a quadlet .container file for a
// per-project custom container. The container joins the lerd network so it
// can reach services (lerd-mysql, lerd-redis, etc.) and is reachable by
// nginx via its container name.
func GenerateCustomContainerQuadlet(siteName, projectPath string, port int) string {
	containerName := CustomContainerName(siteName)
	imageName := CustomImageName(siteName)

	var b strings.Builder

	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=Lerd custom container (%s)\n", siteName)
	b.WriteString("After=network.target\n")

	b.WriteString("\n[Container]\n")
	fmt.Fprintf(&b, "Image=%s\n", imageName)
	fmt.Fprintf(&b, "ContainerName=%s\n", containerName)
	b.WriteString("Network=lerd\n")
	fmt.Fprintf(&b, "Volume=%s:/etc/hosts:ro,z\n", config.ContainerHostsFile())
	fmt.Fprintf(&b, "Volume=%s:%s:rw\n", projectPath, projectPath)
	fmt.Fprintf(&b, "PodmanArgs=--security-opt=label=disable --workdir=%s\n", projectPath)

	b.WriteString("\n[Service]\n")
	b.WriteString("Restart=always\n")

	b.WriteString("\n[Install]\n")
	b.WriteString("WantedBy=default.target\n")

	return b.String()
}

// WriteCustomContainerQuadlet writes the quadlet for a custom container site.
func WriteCustomContainerQuadlet(siteName, projectPath string, port int) error {
	content := GenerateCustomContainerQuadlet(siteName, projectPath, port)
	return WriteContainerUnitFn(CustomContainerName(siteName), content)
}

// RemoveCustomContainerQuadlet removes the unit file for a custom container.
// On Linux this removes the systemd quadlet; on macOS the launchd plist.
func RemoveCustomContainerQuadlet(siteName string) error {
	name := CustomContainerName(siteName)
	// Remove the quadlet file (Linux).
	_ = RemoveQuadlet(name)
	// Also remove the platform-specific unit (macOS plist) via the service
	// manager, which is a no-op on Linux where the quadlet IS the unit.
	if RemoveContainerUnitFn != nil {
		return RemoveContainerUnitFn(name)
	}
	return nil
}

// RemoveContainerUnitFn removes the platform-specific container unit file.
// On macOS this is set to services.Mgr.RemoveContainerUnit to remove the
// launchd plist. Nil on Linux (quadlet removal is sufficient).
var RemoveContainerUnitFn func(name string) error
