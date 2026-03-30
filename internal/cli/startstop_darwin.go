//go:build darwin

package cli

import (
	"fmt"
	"strings"
	"os/exec"

	"github.com/geodro/lerd/internal/podman"
)

// ensurePodmanMachineRunning starts the Podman Machine VM if it isn't already
// running. On macOS, Podman delegates to a Linux VM — containers cannot start
// until the machine is up. This call blocks until the machine is ready.
// It is a no-op when the machine is already running.
func ensurePodmanMachineRunning() {
	out, _ := exec.Command(podman.PodmanBin(), "machine", "list", "--format", "{{.Running}}").Output()
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == "true" {
			return // already running
		}
	}
	fmt.Println("  --> Starting Podman Machine ...")
	cmd := exec.Command(podman.PodmanBin(), "machine", "start")
	if err := cmd.Run(); err != nil {
		fmt.Printf("  WARN: podman machine start: %v\n", err)
	}
}
