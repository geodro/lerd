package podman

import (
	"fmt"
	"os"
	"strings"

	"github.com/geodro/lerd/internal/config"
)

// WriteContainerHosts writes the shared hosts file that is bind-mounted into every
// PHP-FPM container at /etc/hosts. It contains the standard loopback entries,
// host.containers.internal, and one entry per linked site pointing to
// host.containers.internal (169.254.1.2) so that .test domains resolve correctly
// inside containers without requiring a container restart when sites are added or removed.
func WriteContainerHosts() error {
	reg, err := config.LoadSites()
	if err != nil {
		return fmt.Errorf("loading sites: %w", err)
	}

	path := config.ContainerHostsFile()

	// On macOS, WriteContainerUnit pre-creates volume sources as directories.
	// If the hosts path was incorrectly created as a directory, remove it so
	// we can write the file in its place.
	if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("removing stale hosts directory: %w", err)
		}
	}

	var sb strings.Builder
	sb.WriteString("127.0.0.1 localhost\n")
	sb.WriteString("::1 localhost\n")
	sb.WriteString("169.254.1.2 host.containers.internal host.docker.internal\n")

	for _, site := range reg.Sites {
		for _, domain := range site.Domains {
			fmt.Fprintf(&sb, "169.254.1.2 %s\n", domain)
		}
	}

	return os.WriteFile(path, []byte(sb.String()), 0644)
}
