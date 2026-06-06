//go:build darwin

package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/geodro/lerd/internal/podman"
)

// healOverlayCorruptionIfNeeded recovers from the overlay-storage error (see
// isOverlayStorageError) on the service start pass, then asks the caller to
// retry once. Two things are corrupt after an unclean shutdown: the VM's
// overlay base mount, and the lerd-* container layers built on it. A machine
// restart remounts the base; force-removing the stale containers makes the
// retry's `podman run` allocate fresh container storage, the path a manual
// `podman run` takes when it succeeds where a remount alone doesn't. lerd's
// persistent data is host bind-mounted, so both steps are non-destructive.
// Returns true when recovery ran and the caller should retry the start pass.
func healOverlayCorruptionIfNeeded(err error) bool {
	if !isOverlayStorageError(err) {
		return false
	}
	restartPodmanMachineForHeal()
	forceRemoveLerdContainers(true,
		"  --> Clearing stale lerd containers so they rebuild on fresh storage ...")
	return true
}

// restartPodmanMachineForHeal stops and restarts lerd's Podman Machine so its
// container storage is remounted, then refreshes the restart baseline so the
// next run doesn't mistake this restart for an external one.
func restartPodmanMachineForHeal() {
	name := selectedMachineName()
	if name == "" {
		return
	}
	fmt.Println("  --> Container storage looks stale after an unclean shutdown; restarting the Podman Machine to remount it ...")
	stop := exec.Command(podman.PodmanBin(), "machine", "stop", name)
	stop.Stdout = os.Stdout
	stop.Stderr = os.Stderr
	if err := stop.Run(); err != nil {
		fmt.Printf("  WARN: podman machine stop: %v\n", err)
	}
	// ensurePodmanMachineRunning starts the VM and waits for the API socket.
	ensurePodmanMachineRunning()
	recordMachineLastUp()
}

// reportOverlayHealOutcome prints recovery guidance when the overlay-storage
// error persisted after the automatic machine restart and retry, and reports
// whether it claimed the error so the caller can stop the start. It returns
// false for any other error, leaving the normal start flow to continue.
func reportOverlayHealOutcome(err error) bool {
	if !isOverlayStorageError(err) {
		return false
	}
	fmt.Println()
	fmt.Println("  Podman Machine container storage is still corrupted after a restart.")
	fmt.Println("  This happens when the host shuts down while the VM is running.")
	fmt.Println("  Your databases and site data are safe; they live on the host, not in the VM.")
	fmt.Println("  Recreate the VM to fix it (images are rebuilt automatically on the next start):")
	fmt.Println("      lerd machine reset")
	return true
}
