//go:build linux

package cli

import (
	"io"
	"os/exec"
	"strings"

	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
)

// writeDNSUnit writes the container quadlet for the dnsmasq DNS service on Linux.
func writeDNSUnit(_ io.Writer) error {
	content, err := podman.GetQuadletTemplate("lerd-dns.container")
	if err != nil {
		return err
	}
	return services.Mgr.WriteContainerUnit("lerd-dns", content)
}

// ensureDNSImageForStart ensures the lerd-dnsmasq container image exists on Linux.
func ensureDNSImageForStart() {
	podman.EnsureDNSImage() //nolint:errcheck
}

// pullDNSImages returns build jobs to pull alpine and build the dnsmasq container image.
func pullDNSImages() []BuildJob {
	return []BuildJob{
		{
			Label: "Pulling alpine:latest",
			Run: func(w io.Writer) error {
				cmd := exec.Command(podman.PodmanBin(), "pull", "docker.io/library/alpine:latest")
				cmd.Stdout = w
				cmd.Stderr = w
				return cmd.Run()
			},
		},
		{
			Label: "Building dnsmasq image",
			Run: func(w io.Writer) error {
				containerfile := "FROM docker.io/library/alpine:latest\nRUN apk add --no-cache dnsmasq\n"
				cmd := exec.Command(podman.PodmanBin(), "build", "-t", "lerd-dnsmasq:local", "-")
				cmd.Stdin = strings.NewReader(containerfile)
				cmd.Stdout = w
				cmd.Stderr = w
				return cmd.Run()
			},
		},
	}
}

// isDNSContainerUnit returns true on Linux since DNS uses a Podman container.
func isDNSContainerUnit() bool { return true }

// ensureDNSServiceUpdated is a no-op on Linux — DNS always uses a container.
func ensureDNSServiceUpdated(_ io.Writer) error { return nil }

// removeDNSContainerIfRunning is a no-op on Linux.
func removeDNSContainerIfRunning() {}

// nativeDNSRestart is a no-op on Linux — DNS is a container unit managed by systemd.
func nativeDNSRestart() error { return nil }

// needsDNSServiceInstall always returns false on Linux (container quadlet handles it).
func needsDNSServiceInstall() bool { return false }
