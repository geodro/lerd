package podman

import (
	"fmt"
	"os"
	"os/exec"
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

	var sb strings.Builder
	sb.WriteString("127.0.0.1 localhost\n")
	sb.WriteString("::1 localhost\n")
	sb.WriteString("169.254.1.2 host.containers.internal host.docker.internal\n")

	for _, site := range reg.Sites {
		for _, domain := range site.Domains {
			fmt.Fprintf(&sb, "169.254.1.2 %s\n", domain)
		}
	}

	if err := os.WriteFile(config.ContainerHostsFile(), []byte(sb.String()), 0644); err != nil {
		return err
	}

	// Write the browser-testing variant: same domains but resolved to
	// lerd-nginx's IP on the Podman network so Chromium inside Selenium
	// (or similar containers) can reach sites via HTTP/HTTPS.
	return writeBrowserHosts(reg)
}

// writeBrowserHosts writes the browser-testing hosts file. It resolves
// lerd-nginx's IP on the lerd Podman network and maps all .test domains
// to it. If nginx isn't running the file is still written with loopback
// entries (safe no-op — Selenium simply can't reach sites until nginx starts).
func writeBrowserHosts(reg *config.SiteRegistry) error {
	nginxIP := nginxContainerIP()

	var sb strings.Builder
	sb.WriteString("127.0.0.1 localhost\n")
	sb.WriteString("::1 localhost\n")

	for _, site := range reg.Sites {
		for _, domain := range site.Domains {
			fmt.Fprintf(&sb, "%s %s\n", nginxIP, domain)
		}
	}

	return os.WriteFile(config.BrowserHostsFile(), []byte(sb.String()), 0644)
}

// nginxContainerIP returns the IP address of lerd-nginx on the lerd Podman
// network. Falls back to 127.0.0.1 if the container isn't running.
func nginxContainerIP() string {
	out, err := exec.Command(PodmanBin(), "inspect", "lerd-nginx",
		"--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}").Output()
	if err != nil {
		return "127.0.0.1"
	}
	ip := strings.TrimSpace(string(out))
	if ip == "" {
		return "127.0.0.1"
	}
	return ip
}
