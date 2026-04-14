package podman

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/geodro/lerd/internal/config"
)

// Fallback for podman rootless + pasta/netavark/slirp4netns.
const fallbackHostGatewayIP = "169.254.1.2"

// WriteContainerHosts writes the shared /etc/hosts bind-mounted into every
// PHP-FPM container. host.containers.internal uses the probed host gateway;
// .test domains point at lerd-nginx directly on the lerd bridge network.
func WriteContainerHosts() error {
	reg, err := config.LoadSites()
	if err != nil {
		return fmt.Errorf("loading sites: %w", err)
	}

	hostIP := DetectHostGatewayIP()
	nginxIP := nginxContainerIP()

	content := renderContainerHosts(reg, hostIP, nginxIP)
	if err := os.WriteFile(config.ContainerHostsFile(), []byte(content), 0644); err != nil {
		return err
	}

	// Write the browser-testing variant: same domains but resolved to
	// lerd-nginx's IP on the Podman network so Chromium inside Selenium
	// (or similar containers) can reach sites via HTTP/HTTPS.
	return writeBrowserHosts(reg)
}

// renderContainerHosts builds the /etc/hosts contents for PHP-FPM containers.
// .test domains go to nginxIP (direct bridge), host.containers.internal to
// hostIP (host gateway for Xdebug and other host-side services).
func renderContainerHosts(reg *config.SiteRegistry, hostIP, nginxIP string) string {
	var sb strings.Builder
	sb.WriteString("127.0.0.1 localhost\n")
	sb.WriteString("::1 localhost\n")
	fmt.Fprintf(&sb, "%s host.containers.internal host.docker.internal\n", hostIP)

	for _, site := range reg.Sites {
		for _, domain := range site.Domains {
			fmt.Fprintf(&sb, "%s %s\n", nginxIP, domain)
		}
	}
	return sb.String()
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

// DetectHostGatewayIP returns the IP podman uses for host.containers.internal
// on the lerd network. Tries an exec into lerd-nginx, then a throwaway alpine
// probe, and finally falls back to fallbackHostGatewayIP.
func DetectHostGatewayIP() string {
	if ip := parseHostGatewayFromExec("lerd-nginx"); ip != "" {
		return ip
	}
	if ip := parseHostGatewayFromProbe(); ip != "" {
		return ip
	}
	return fallbackHostGatewayIP
}

func parseHostGatewayFromExec(container string) string {
	out, err := exec.Command(PodmanBin(), "exec", container,
		"getent", "hosts", "host.containers.internal").Output()
	if err != nil {
		return ""
	}
	return firstField(string(out))
}

func parseHostGatewayFromProbe() string {
	out, err := exec.Command(PodmanBin(), "run", "--rm", "--network", "lerd",
		"docker.io/library/alpine", "getent", "hosts", "host.containers.internal").Output()
	if err != nil {
		return ""
	}
	return firstField(string(out))
}

func firstField(s string) string {
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 {
			return fields[0]
		}
	}
	return ""
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
