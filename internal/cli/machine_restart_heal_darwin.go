//go:build darwin

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// machineLastUpFile records the Podman Machine LastUp from the prior run.
// Compared on the next run to detect a machine restart that orphaned
// gvproxy's host-side port forwards.
func machineLastUpFile() string {
	return filepath.Join(config.DataDir(), "podman-machine-lastup")
}

// selectedMachineName mirrors ensurePodmanMachineRunning's selection order
// (default-marked, else first) so inspect targets the VM lerd actually
// uses. Returns "" when no machine exists yet.
func selectedMachineName() string {
	out, err := exec.Command(podman.PodmanBin(), "machine", "list",
		"--format", "{{.Name}}").Output()
	if err != nil {
		return ""
	}
	var first, def string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		raw := strings.TrimSpace(line)
		if raw == "" {
			continue
		}
		name := strings.TrimSuffix(raw, "*")
		if first == "" {
			first = name
		}
		if strings.HasSuffix(raw, "*") {
			def = name
			break
		}
	}
	if def != "" {
		return def
	}
	return first
}

// currentMachineLastUp returns the LastUp of lerd's Podman Machine, or ""
// when the machine doesn't exist or podman can't report it. The name is
// pinned explicitly so other machines don't perturb the comparison.
func currentMachineLastUp() string {
	name := selectedMachineName()
	if name == "" {
		return ""
	}
	out, err := exec.Command(podman.PodmanBin(), "machine", "inspect",
		"--format", "{{.LastUp}}", name).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// recordMachineLastUp persists the current LastUp so the next run has a
// baseline to compare against.
func recordMachineLastUp() {
	cur := currentMachineLastUp()
	if cur == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(machineLastUpFile()), 0755)
	_ = os.WriteFile(machineLastUpFile(), []byte(cur), 0644)
}

// readPriorMachineLastUp returns the LastUp recorded on the prior run, or
// "" if the state file is missing or unreadable.
func readPriorMachineLastUp() string {
	data, err := os.ReadFile(machineLastUpFile())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// shouldHealAfterMachineStart returns true when the machine was restarted
// externally between runs; comparing the pre-ensure LastUp rules out
// stop+starts the ensure itself performs.
func shouldHealAfterMachineStart(preEnsureLastUp, priorBaseline string) bool {
	if priorBaseline == "" {
		return false
	}
	if preEnsureLastUp == "" {
		return true
	}
	return preEnsureLastUp != priorBaseline
}

// healMachineRestartIfNeeded force-removes running lerd-* containers when
// the machine was restarted externally, then records the post-ensure
// LastUp so an interrupted run still leaves a usable baseline.
func healMachineRestartIfNeeded(preEnsureLastUp string) {
	if shouldHealAfterMachineStart(preEnsureLastUp, readPriorMachineLastUp()) {
		removeLerdContainersForGvproxyHeal()
	}
	recordMachineLastUp()
}

// removeLerdContainersForGvproxyHeal force-removes every running lerd-*
// container so the next StartUnit pass invokes `podman run -p` fresh from
// the host and gvproxy re-registers the host port forwards.
func removeLerdContainersForGvproxyHeal() {
	forceRemoveLerdContainers(false,
		"  --> Podman Machine was restarted; recreating containers to restore host port forwards ...")
}

// forceRemoveLerdContainers force-removes lerd-* containers so the next
// StartUnit pass recreates them fresh via `podman run`. includeStopped adds
// `-a` to also catch created/exited containers, used by the overlay heal,
// where the failed containers never reached running state. announce is
// printed only when at least one container matched.
func forceRemoveLerdContainers(includeStopped bool, announce string) {
	psArgs := []string{"ps", "--format", "{{.Names}}", "--filter", "name=^lerd-"}
	if includeStopped {
		psArgs = append(psArgs, "-a")
	}
	out, err := podman.Run(psArgs...)
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
	fmt.Println(announce)
	args := append([]string{"rm", "-f"}, names...)
	if out, err := exec.Command(podman.PodmanBin(), args...).CombinedOutput(); err != nil {
		fmt.Printf("       WARN: podman rm -f failed: %v\n%s\n", err, strings.TrimSpace(string(out)))
	}
}
