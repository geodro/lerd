//go:build darwin

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/geodro/lerd/internal/podman"
)

// ensurePodmanMachineRunning ensures a Podman Machine VM exists and is running.
// If no machine exists it initialises one first. On macOS all container
// operations require the VM to be up; this call blocks until it is ready.
func ensurePodmanMachineRunning() {
	out, _ := exec.Command(podman.PodmanBin(), "machine", "list", "--format", "{{.Name}}\t{{.Running}}").Output()

	machineExists := false
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		machineExists = true
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "true" {
			return // already running
		}
	}

	if !machineExists {
		fmt.Println("  --> Initialising Podman Machine (first run, this may take a minute) ...")
		cmd := exec.Command(podman.PodmanBin(), "machine", "init")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf("  WARN: podman machine init: %v\n", err)
			return
		}
	}

	fmt.Println("  --> Starting Podman Machine ...")
	cmd := exec.Command(podman.PodmanBin(), "machine", "start")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("  WARN: podman machine start: %v\n", err)
	}
}
