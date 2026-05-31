//go:build darwin

package cli

import (
	"crypto/sha256"
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

// checkMissingMounts parses the Podman Machine JSON configuration file and returns
// true if any of the essential default macOS mounts (/private, /var/folders, /Volumes)
// are missing from the configuration. This identifies existing machines that were
// initialized before the /Volumes mount requirement was added.
func checkMissingMounts(name string) bool {
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

	hasVolumes := false
	hasPrivate := false
	hasVarFolders := false

	for _, mAny := range mounts {
		if m, ok := mAny.(map[string]any); ok {
			src, _ := m["Source"].(string)
			if src == "/Volumes" {
				hasVolumes = true
			}
			if src == "/private" {
				hasPrivate = true
			}
			if src == "/var/folders" {
				hasVarFolders = true
			}
		}
	}
	return !hasVolumes || !hasPrivate || !hasVarFolders
}

// ensurePodmanMachineMounts edits the Podman Machine JSON configuration file directly
// to inject missing volume mounts for /private, /var/folders, and /Volumes.
// It generates deterministic virtiofs tags based on the SHA-256 hash of the host path,
// allowing existing machines to be upgraded seamlessly without requiring a rebuild.
// A backup of the original configuration is saved with a .bak extension.
func ensurePodmanMachineMounts(name string) error {
	path := getMachineJSONPath(name)
	if path == "" {
		return fmt.Errorf("machine config not found")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}
	mounts, ok := config["Mounts"].([]any)
	if !ok {
		return fmt.Errorf("no Mounts array in config")
	}

	existingPaths := make(map[string]bool)
	for _, mAny := range mounts {
		if m, ok := mAny.(map[string]any); ok {
			src, _ := m["Source"].(string)
			existingPaths[src] = true
		}
	}

	requiredPaths := []string{"/private", "/var/folders", "/Volumes"}
	added := false

	for _, reqPath := range requiredPaths {
		if !existingPaths[reqPath] {
			hash := sha256.Sum256([]byte(reqPath))
			tag := fmt.Sprintf("%x", hash)[:36]
			newMount := map[string]any{
				"OriginalInput": "",
				"ReadOnly":      false,
				"Source":        reqPath,
				"Tag":           tag,
				"Target":        reqPath,
				"Type":          "virtiofs",
				"VSockNumber":   nil,
			}
			mounts = append(mounts, newMount)
			added = true
		}
	}

	if !added {
		return nil
	}

	config["Mounts"] = mounts

	outData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	// Backup original
	os.WriteFile(path+".bak", data, 0644) //nolint:errcheck

	return os.WriteFile(path, outData, 0644)
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
		cmd := exec.Command(podman.PodmanBin(), "machine", "init", "--rootful",
			"-v", "/Users:/Users",
			"-v", "/private:/private",
			"-v", "/var/folders:/var/folders",
			"-v", "/Volumes:/Volumes")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf("  WARN: podman machine init: %v\n", err)
			return
		}
	} else {
		m := machines[0]

		needsRootful := !m.rootful
		needsMemory := false
		needsMounts := checkMissingMounts(m.name)

		// Target memory scales with host RAM so 8 GB MacBooks aren't squeezed.
		// {{.Resources.Memory}} returns MiB directly (not bytes).
		hostGiB := hostMemoryGiB()
		cfg, _ := config.LoadGlobal()
		execMode := cfg != nil && cfg.WorkerExecMode() != config.WorkerExecModeContainer
		targetMemoryMiB := recommendedVMMemoryMiB(hostGiB, execMode)
		if inspectMem, err := exec.Command(podman.PodmanBin(), "machine", "inspect",
			"--format", "{{.Resources.Memory}}", m.name).Output(); err == nil {
			if memMiB, parseErr := strconv.ParseInt(strings.TrimSpace(string(inspectMem)), 10, 64); parseErr == nil && memMiB > 0 {
				if memMiB < targetMemoryMiB {
					needsMemory = true
				}
			}
		}

		if needsRootful || needsMemory || needsMounts {
			if m.running {
				var reason string
				switch {
				case needsRootful && needsMemory && needsMounts:
					reason = fmt.Sprintf("enable rootful mode, increase memory to %d MB, and update mounts", targetMemoryMiB)
				case needsRootful && needsMemory:
					reason = fmt.Sprintf("enable rootful mode and increase memory to %d MB", targetMemoryMiB)
				case needsMounts && needsMemory:
					reason = fmt.Sprintf("increase memory to %d MB and update mounts", targetMemoryMiB)
				case needsMounts && needsRootful:
					reason = "enable rootful mode and update mounts"
				case needsRootful:
					reason = "enable rootful mode"
				case needsMounts:
					reason = "update volume mounts for external drives"
				default:
					reason = fmt.Sprintf("increase memory to %d MB", targetMemoryMiB)
				}
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
			if needsMounts {
				fmt.Println("  --> Updating Podman Machine volume mounts for external drives ...")
				if err := ensurePodmanMachineMounts(m.name); err != nil {
					fmt.Printf("  WARN: failed to update machine mounts: %v\n", err)
				}
			}
		} else if m.running {
			return // already running and correctly configured
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
