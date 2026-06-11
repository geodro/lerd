//go:build darwin

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// migrateExecWorkerPlists removes exec-based worker plists. On macOS, workers
// now run as independent detached containers (podman run -d) rather than
// exec'ing into the PHP-FPM container. Removing the old exec-based plists
// lets restoreSiteInfrastructure recreate them in the container format.
func migrateExecWorkerPlists() {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library", "LaunchAgents")
	for _, glob := range []string{"lerd-queue-*.plist", "lerd-schedule-*.plist", "lerd-reverb-*.plist", "lerd-horizon-*.plist"} {
		matches, _ := filepath.Glob(filepath.Join(dir, glob))
		for _, p := range matches {
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			// Only remove exec-based plists; container-based plists use "run" not "exec".
			if !strings.Contains(string(data), "<string>exec</string>") {
				continue
			}
			name := strings.TrimSuffix(filepath.Base(p), ".plist")
			domain := fmt.Sprintf("gui/%d", os.Getuid())
			exec.Command("launchctl", "bootout", domain+"/com.lerd."+name).Run() //nolint:errcheck
			os.Remove(p)                                                         //nolint:errcheck
		}
	}
}

// hostMemoryGiB reads host RAM in GiB via sysctl. Returns 0 on failure so
// the caller falls back to the safe 4 GB default.
func hostMemoryGiB() int {
	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0
	}
	bytes, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil || bytes <= 0 {
		return 0
	}
	return int(bytes / (1024 * 1024 * 1024))
}

// getMachineJSONPath locates the underlying Podman Machine JSON configuration file
// (e.g. ~/.config/containers/podman/machine/applehv/podman-machine-default.json)
// for the given machine name. Returns an empty string if not found.
func getMachineJSONPath(name string) string {
	home, _ := os.UserHomeDir()
	matches, _ := filepath.Glob(filepath.Join(home, ".config", "containers", "podman", "machine", "*", name+".json"))
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}

// requiredMachineMounts lists the macOS host paths mounted into the Podman
// Machine VM at init. /Users carries the host home (and therefore
// ~/.local/share/lerd, the source of every container bind mount); /private and
// /var/folders are Podman's other defaults; /Volumes covers external drives.
//
// All four are passed explicitly to `machine init -v`. Podman's --volume is a
// stringArray, so supplying any -v replaces the whole default set
// (/Users, /private, /var/folders); passing only /Volumes (lerd <= 1.24.0)
// dropped the host home mount, and every bind mount sourced from
// ~/.local/share/lerd failed inside the VM with "statfs ...: no such file or
// directory".
var requiredMachineMounts = []string{"/Users", "/private", "/var/folders", "/Volumes"}

// homeMachineMount is the host path that carries the user home inside the VM.
// A machine missing it can't see any lerd bind-mount source, so no container
// can start.
const homeMachineMount = "/Users"

// machineInitArgs builds `podman machine init` arguments with every required
// mount and the host-scaled memory. name re-creates a specific machine
// (preserving a custom name); "" creates Podman's default.
func machineInitArgs(name string, targetMemoryMiB int64) []string {
	args := []string{"machine", "init", "--rootful"}
	for _, m := range requiredMachineMounts {
		args = append(args, "-v", m+":"+m)
	}
	if targetMemoryMiB > 0 {
		args = append(args, "--memory", strconv.FormatInt(targetMemoryMiB, 10))
	}
	if name != "" {
		args = append(args, name)
	}
	return args
}

// machineMissingHomeMount reports whether the named machine's config lacks the
// host home mount, i.e. it was initialised by the lerd <= 1.24.0 bug. Returns
// false on any read/parse error so we never recreate a machine we can't
// positively diagnose as broken.
//
// This must be repaired by recreating the VM, not by editing the config:
// Podman writes the guest's virtiofs .mount units once at init via Ignition and
// `machine start` never regenerates them, so adding /Users to the config JSON
// attaches the host-side device but leaves the guest with no mount unit; the
// path still never appears inside the VM.
func machineMissingHomeMount(name string) bool {
	path := getMachineJSONPath(name)
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return false
	}
	mounts, ok := config["Mounts"].([]any)
	if !ok {
		return false
	}
	for _, mAny := range mounts {
		if m, ok := mAny.(map[string]any); ok {
			if src, _ := m["Source"].(string); src == homeMachineMount {
				return false // home mount present
			}
		}
	}
	return true
}

// recreateBrokenMachine destroys and re-initialises a machine that is missing
// the host home mount. Safe because such a machine has no working containers
// (every bind mount fails), so nothing of value is lost. The caller starts the
// freshly-initialised machine.
func recreateBrokenMachine(name string, running bool, targetMemoryMiB int64) {
	fmt.Println("  --> Podman Machine is missing the host home mount and can't be repaired")
	fmt.Println("      in place; recreating it. No running containers are affected (a")
	fmt.Println("      machine in this state can't start any).")
	if running {
		stopCmd := exec.Command(podman.PodmanBin(), "machine", "stop", name)
		stopCmd.Stdout = os.Stdout
		stopCmd.Stderr = os.Stderr
		stopCmd.Run() //nolint:errcheck
	}
	rmCmd := exec.Command(podman.PodmanBin(), "machine", "rm", "-f", name)
	rmCmd.Stdout = os.Stdout
	rmCmd.Stderr = os.Stderr
	if err := rmCmd.Run(); err != nil {
		fmt.Printf("  WARN: podman machine rm: %v\n", err)
		return
	}
	initCmd := exec.Command(podman.PodmanBin(), machineInitArgs(name, targetMemoryMiB)...)
	initCmd.Stdout = os.Stdout
	initCmd.Stderr = os.Stderr
	if err := initCmd.Run(); err != nil {
		fmt.Printf("  WARN: podman machine init: %v\n", err)
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
		// Size memory at init so a fresh VM (first run, or one recreated by
		// `lerd machine reset`) boots at the host-scaled target rather than
		// podman's stock default. The existing-machine branch below only
		// resizes machines that already exist.
		cfg, _ := config.LoadGlobal()
		execMode := cfg != nil && cfg.WorkerExecMode() != config.WorkerExecModeContainer
		targetMemoryMiB := recommendedVMMemoryMiB(hostMemoryGiB(), execMode)
		cmd := exec.Command(podman.PodmanBin(), machineInitArgs("", targetMemoryMiB)...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf("  WARN: podman machine init: %v\n", err)
			return
		}
	} else {
		m := machines[0]

		// Target memory scales with host RAM so 8 GB MacBooks aren't squeezed.
		// {{.Resources.Memory}} returns MiB directly (not bytes).
		hostGiB := hostMemoryGiB()
		cfg, _ := config.LoadGlobal()
		execMode := cfg != nil && cfg.WorkerExecMode() != config.WorkerExecModeContainer
		targetMemoryMiB := recommendedVMMemoryMiB(hostGiB, execMode)

		// A machine missing the host home mount was created by the lerd <= 1.24.0
		// init bug and can't be repaired in place (Ignition writes the guest
		// mount units once at init; a config edit + restart won't add /Users).
		// Recreate it, then fall through to start.
		if machineMissingHomeMount(m.name) {
			recreateBrokenMachine(m.name, m.running, targetMemoryMiB)
		} else {
			needsRootful := !m.rootful
			needsMemory := false
			if inspectMem, err := exec.Command(podman.PodmanBin(), "machine", "inspect",
				"--format", "{{.Resources.Memory}}", m.name).Output(); err == nil {
				if memMiB, parseErr := strconv.ParseInt(strings.TrimSpace(string(inspectMem)), 10, 64); parseErr == nil && memMiB > 0 {
					if memMiB < targetMemoryMiB {
						needsMemory = true
					}
				}
			}

			if needsRootful || needsMemory {
				if m.running {
					var parts []string
					if needsRootful {
						parts = append(parts, "enable rootful mode")
					}
					if needsMemory {
						parts = append(parts, fmt.Sprintf("increase memory to %d MB", targetMemoryMiB))
					}
					reason := strings.Join(parts, " and ")
					fmt.Printf("  --> Stopping Podman Machine to %s ...\n", reason)
					stopCmd := exec.Command(podman.PodmanBin(), "machine", "stop", m.name)
					stopCmd.Stdout = os.Stdout
					stopCmd.Stderr = os.Stderr
					stopCmd.Run() //nolint:errcheck
				}
				if needsRootful {
					fmt.Println("  --> Enabling rootful mode for Podman Machine (required for ports 80/443) ...")
					setCmd := exec.Command(podman.PodmanBin(), "machine", "set", "--rootful", m.name)
					setCmd.Stdout = os.Stdout
					setCmd.Stderr = os.Stderr
					if err := setCmd.Run(); err != nil {
						fmt.Printf("  WARN: podman machine set --rootful: %v\n", err)
					}
				}
				if needsMemory {
					if hostGiB > 0 && hostGiB <= 8 {
						fmt.Printf("  --> Host has %d GB RAM; setting Podman Machine to %d MB (tight but workable) ...\n", hostGiB, targetMemoryMiB)
						fmt.Println("       If sites slow down under load, run: podman machine set --memory 4096")
					} else {
						fmt.Printf("  --> Setting Podman Machine memory to %d MB ...\n", targetMemoryMiB)
					}
					setCmd := exec.Command(podman.PodmanBin(), "machine", "set",
						"--memory", strconv.FormatInt(targetMemoryMiB, 10), m.name)
					setCmd.Stdout = os.Stdout
					setCmd.Stderr = os.Stderr
					if err := setCmd.Run(); err != nil {
						fmt.Printf("  WARN: podman machine set --memory: %v\n", err)
					}
				}
			} else if m.running {
				return // already running and correctly configured
			}
		}
	}

	fmt.Println("  --> Starting Podman Machine ...")
	cmd := exec.Command(podman.PodmanBin(), "machine", "start")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("  WARN: podman machine start: %v\n", err)
		return
	}

	// `podman machine start` exits before the API socket is ready to handle
	// container operations. Poll `podman ps` (which exercises the full
	// container stack, not just the info endpoint) until it succeeds, then
	// wait a few extra seconds for the socket to fully settle.
	fmt.Print("  --> Waiting for Podman Machine to be ready ...")
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		if err := exec.Command(podman.PodmanBin(), "ps", "-q").Run(); err == nil {
			time.Sleep(3 * time.Second) // grace period before container ops
			fmt.Println(" ready")
			return
		}
		time.Sleep(500 * time.Millisecond)
		fmt.Print(".")
	}
	fmt.Println(" timed out (proceeding anyway)")
}

// stopPodmanMachine stops the running Podman Machine VM. Called by runQuit so
// the VM is cleanly shut down when the user quits Lerd entirely.
func stopPodmanMachine() {
	out, err := exec.Command(podman.PodmanBin(), "machine", "list", "--format", "{{.Name}}\t{{.Running}}").Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[1] != "true" {
			continue
		}
		name := strings.TrimSuffix(fields[0], "*")
		fmt.Printf("  --> Stopping Podman Machine (%s) ...\n", name)
		cmd := exec.Command(podman.PodmanBin(), "machine", "stop", name)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf("  WARN: podman machine stop: %v\n", err)
		}
	}
}

// batchStopContainers stops all running lerd-* containers in two podman calls
// (stop then rm) so the Podman Machine socket isn't flooded by N individual
// stop requests from RunParallel. After this returns the individual Stop()
// calls find no containers and go straight to launchctl bootout.
func batchStopContainers(_ []string) {
	// Query only running containers with name prefix "lerd-" to avoid passing
	// non-existent names (native services like lerd-dns have no container).
	out, err := podman.Run("ps", "--format", "{{.Names}}", "--filter", "name=^lerd-")
	if err != nil || strings.TrimSpace(out) == "" {
		return
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if n := strings.TrimSpace(line); n != "" {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return
	}
	podman.RunSilent(append([]string{"stop", "-t", "5"}, names...)...) //nolint:errcheck
	podman.RunSilent(append([]string{"rm", "-f"}, names...)...)        //nolint:errcheck
}
