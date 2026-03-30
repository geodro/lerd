//go:build darwin

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/geodro/lerd/internal/podman"
)

// ensurePodmanMachineRunning ensures a Podman Machine VM exists, is rootful,
// and is running. If no machine exists it initialises one with --rootful.
// If an existing machine is rootless it is stopped, switched, and restarted.
// On macOS all container operations require the VM to be up.
func ensurePodmanMachineRunning() {
	type machineInfo struct {
		name    string
		running bool
		rootful bool
	}

	out, _ := exec.Command(podman.PodmanBin(), "machine", "list", "--format", "{{.Name}}\t{{.Running}}\t{{.Rootful}}").Output()

	var machines []machineInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		machines = append(machines, machineInfo{
			name:    fields[0],
			running: fields[1] == "true",
			rootful: fields[2] == "true",
		})
	}

	if len(machines) == 0 {
		fmt.Println("  --> Initialising Podman Machine (first run, this may take a minute) ...")
		cmd := exec.Command(podman.PodmanBin(), "machine", "init", "--rootful")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf("  WARN: podman machine init: %v\n", err)
			return
		}
	} else {
		m := machines[0]
		if !m.rootful {
			// Switch to rootful — required for nginx to bind ports 80/443.
			if m.running {
				fmt.Println("  --> Stopping Podman Machine to enable rootful mode ...")
				stopCmd := exec.Command(podman.PodmanBin(), "machine", "stop", m.name)
				stopCmd.Stdout = os.Stdout
				stopCmd.Stderr = os.Stderr
				stopCmd.Run() //nolint:errcheck
			}
			fmt.Println("  --> Enabling rootful mode for Podman Machine (required for ports 80/443) ...")
			setCmd := exec.Command(podman.PodmanBin(), "machine", "set", "--rootful", m.name)
			setCmd.Stdout = os.Stdout
			setCmd.Stderr = os.Stderr
			if err := setCmd.Run(); err != nil {
				fmt.Printf("  WARN: podman machine set --rootful: %v\n", err)
			}
		} else if m.running {
			return // already running and rootful
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
