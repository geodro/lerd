//go:build darwin

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/podman"
)

// migrateExecWorkerPlists removes legacy worker plists that used `podman exec`
// (written by alpha.2/alpha.3). These cause "no such container" errors on every
// podman invocation because the container they exec into no longer exists.
// Workers must be re-registered after removal (e.g. `lerd schedule:start`).
func migrateExecWorkerPlists() {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library", "LaunchAgents")
	for _, glob := range []string{"lerd-queue-*.plist", "lerd-schedule-*.plist", "lerd-reverb-*.plist"} {
		matches, _ := filepath.Glob(filepath.Join(dir, glob))
		for _, p := range matches {
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			if !strings.Contains(string(data), "<string>exec</string>") {
				continue
			}
			name := strings.TrimSuffix(filepath.Base(p), ".plist")
			domain := fmt.Sprintf("gui/%d", os.Getuid())
			exec.Command("launchctl", "bootout", domain+"/com.lerd."+name).Run() //nolint:errcheck
			os.Remove(p)                                                          //nolint:errcheck
			fmt.Printf("  --> Removed stale exec-based plist %s — re-register with `lerd schedule:start` / `lerd queue:start`\n", name)
		}
	}
}

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

	type machineEntry struct {
		machineInfo
		isDefault bool
	}

	var all []machineEntry
	for _, line := range strings.Split(strings.TrimSpace(string(listOut)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		raw := fields[0]
		isDefault := strings.HasSuffix(raw, "*")
		name := strings.TrimSuffix(raw, "*")
		running := fields[1] == "true"

		// Inspect to get Rootful status.
		rootful := false
		inspectOut, err := exec.Command(podman.PodmanBin(), "machine", "inspect", "--format", "{{.Rootful}}", name).Output()
		if err == nil {
			rootful = strings.TrimSpace(string(inspectOut)) == "true"
		}

		all = append(all, machineEntry{machineInfo{name, running, rootful}, isDefault})
	}

	// Prefer the default machine (marked with *); fall back to the first listed.
	var machines []machineInfo
	for _, e := range all {
		if e.isDefault {
			machines = []machineInfo{e.machineInfo}
			break
		}
	}
	if len(machines) == 0 && len(all) > 0 {
		machines = []machineInfo{all[0].machineInfo}
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

	// Prune stopped containers and unused resources after a machine (re)start to
	// clear any stale exec sessions left in Podman's database from processes
	// that were killed abruptly.
	podman.RunSilent("system", "prune", "-f") //nolint:errcheck
}
