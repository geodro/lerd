package podman

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

type machineInfo struct {
	Name     string `json:"Name"`
	Running  bool   `json:"Running"`
	Starting bool   `json:"Starting"`
}

// EnsureMachineRunning checks whether the Podman machine is reachable on
// macOS and starts it if it is not. It is a no-op on Linux.
func EnsureMachineRunning() error {
	if runtime.GOOS != "darwin" {
		return nil
	}

	// Fast path: socket is already up.
	if RunSilent("info") == nil {
		return nil
	}

	// Socket is down. Find a machine to start.
	// podman machine list reads local config and does not need the socket.
	out, err := exec.Command(podmanBin(), "machine", "list", "--format", "json").Output()
	if err != nil {
		return fmt.Errorf("Podman is not running and machine list failed.\nRun: podman machine start")
	}

	var machines []machineInfo
	if err := json.Unmarshal(out, &machines); err != nil || len(machines) == 0 {
		return fmt.Errorf("no Podman machine found — run: podman machine init && podman machine start")
	}

	// Prefer the default machine; fall back to first.
	m := machines[0]
	for _, candidate := range machines {
		if candidate.Name == "podman-machine-default" {
			m = candidate
			break
		}
	}

	if m.Running || m.Starting {
		// Machine thinks it is running but the socket is not yet up — just wait.
		fmt.Printf("  --> podman machine %q starting ...\n", m.Name)
	} else {
		fmt.Printf("  --> podman machine %q is stopped, starting it ...\n", m.Name)
		cmd := exec.Command(podmanBin(), "machine", "start", m.Name)
		if startOut, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("podman machine start: %w\n%s", err, string(startOut))
		}
	}

	// Wait up to 30 s for the socket to become ready.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if RunSilent("info") == nil {
			fmt.Println("  --> podman machine ready")
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("podman machine started but not ready after 30s")
}
