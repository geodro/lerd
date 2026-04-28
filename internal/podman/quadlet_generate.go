package podman

import (
	"fmt"
	"sort"
	"strings"

	"github.com/geodro/lerd/internal/config"
)

// GenerateCustomQuadlet builds a quadlet .container file for a custom service.
func GenerateCustomQuadlet(svc *config.CustomService) string {
	var b strings.Builder

	b.WriteString("[Unit]\n")
	desc := svc.Description
	if desc == "" {
		desc = "Lerd " + svc.Name
	}
	fmt.Fprintf(&b, "Description=%s\n", desc)
	b.WriteString("After=network.target\n")

	b.WriteString("\n[Container]\n")
	fmt.Fprintf(&b, "Image=%s\n", svc.Image)
	fmt.Fprintf(&b, "ContainerName=lerd-%s\n", svc.Name)
	b.WriteString("Network=lerd\n")
	// Bound podman's graceful-stop window so images with slow shutdown
	// sequences (selenium/supervisord, chromium) don't block systemctl stop
	// for the full 90 s default. Mirrors the --stop-timeout=5 used on macOS.
	b.WriteString("StopTimeout=5\n")

	if svc.ShareHosts {
		fmt.Fprintf(&b, "Volume=%s:/etc/hosts:ro,z\n", config.BrowserHostsFile())
	}

	for _, port := range svc.Ports {
		fmt.Fprintf(&b, "PublishPort=%s\n", port)
	}

	if svc.DataDir != "" {
		hostDir := config.DataSubDir(svc.Name)
		flags := "z"
		if svc.ChownData {
			flags += ",U"
		}
		fmt.Fprintf(&b, "Volume=%s:%s:%s\n", hostDir, svc.DataDir, flags)
	}

	if svc.Userns != "" {
		fmt.Fprintf(&b, "UserNS=%s\n", svc.Userns)
	}

	for _, f := range config.PresetFiles(svc.Preset) {
		hostPath := config.ServiceFilePath(svc.Name, f.Target)
		flags := "z"
		if f.Chown {
			flags += ",U"
		}
		fmt.Fprintf(&b, "Volume=%s:%s:%s\n", hostPath, f.Target, flags)
	}

	envKeys := make([]string, 0, len(svc.Environment))
	for k := range svc.Environment {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		// systemd splits Environment= on whitespace and strips unescaped
		// double quotes, so JSON / quoted-wildcard values get mangled.
		// Wrap the whole pair and escape inner quotes to preserve them.
		escaped := strings.ReplaceAll(svc.Environment[k], `"`, `\"`)
		fmt.Fprintf(&b, "Environment=\"%s=%s\"\n", k, escaped)
	}

	if svc.Exec != "" {
		fmt.Fprintf(&b, "Exec=%s\n", svc.Exec)
	}

	b.WriteString("\n[Service]\n")
	b.WriteString("Restart=always\n")

	b.WriteString("\n[Install]\n")
	b.WriteString("WantedBy=default.target\n")

	return b.String()
}
