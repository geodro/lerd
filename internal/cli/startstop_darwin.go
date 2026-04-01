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
	// machine list only exposes Name and Running; use inspect for Rootful.
	listOut, _ := exec.Command(podman.PodmanBin(), "machine", "list", "--format", "{{.Name}}\t{{.Running}}").Output()

	type machineInfo struct {
		name    string
		running bool
		rootful bool
	}

	var machines []machineInfo
	for _, line := range strings.Split(strings.TrimSpace(string(listOut)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimSuffix(fields[0], "*") // strip default-machine marker
		running := fields[1] == "true"

		// Inspect to get Rootful status.
		rootful := false
		inspectOut, err := exec.Command(podman.PodmanBin(), "machine", "inspect", "--format", "{{.Rootful}}", name).Output()
		if err == nil {
			rootful = strings.TrimSpace(string(inspectOut)) == "true"
		}

		machines = append(machines, machineInfo{name: name, running: running, rootful: rootful})
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

	// Prune stopped containers after a machine (re)start to clear any stale exec
	// sessions left in Podman's database from processes that were killed abruptly.
	podman.RunSilent("container", "prune", "-f") //nolint:errcheck
}
