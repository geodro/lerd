//go:build darwin

package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/geodro/lerd/internal/feedback"
	"github.com/geodro/lerd/internal/podman"
)

// runMachineReset stops and removes lerd's Podman Machine, then recreates it.
// The VM's container storage (the source of overlay corruption after an
// unclean shutdown) is discarded; lerd's persistent data lives in host
// bind-mounts and survives. Images are rebuilt on the next lerd start.
func runMachineReset(assumeYes bool) error {
	name := selectedMachineName()
	if name == "" {
		fmt.Println("No Podman Machine found; nothing to reset. Run `lerd start` to create one.")
		return nil
	}

	if !assumeYes {
		fmt.Printf("This recreates the Podman Machine %q.\n", name)
		fmt.Println("Databases and site data are preserved (stored on the host).")
		fmt.Println("Container images and any non-lerd containers in this VM are removed and rebuilt on next start.")
		fmt.Print("Continue? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		if ans != "y" && ans != "yes" {
			// Declining the prompt is a normal choice, not a failure, so print
			// a plain notice and exit zero rather than letting cobra render
			// "Error: aborted" with a non-zero status.
			fmt.Println("Cancelled.")
			return nil
		}
	}

	feedback.Line(fmt.Sprintf("Stopping Podman Machine %q…", name))
	stop := exec.Command(podman.PodmanBin(), "machine", "stop", name)
	stop.Stdout = os.Stdout
	stop.Stderr = os.Stderr
	_ = stop.Run() // a stopped machine still removes fine

	feedback.Line(fmt.Sprintf("Removing Podman Machine %q…", name))
	rm := exec.Command(podman.PodmanBin(), "machine", "rm", "-f", name)
	rm.Stdout = os.Stdout
	rm.Stderr = os.Stderr
	if err := rm.Run(); err != nil {
		return fmt.Errorf("podman machine rm %s: %w", name, err)
	}

	// ensurePodmanMachineRunning re-inits a rootful machine when none exists
	// and waits for the API socket to be ready.
	_ = ensurePodmanMachineRunning()
	recordMachineLastUp()

	fmt.Println()
	fmt.Println("Podman Machine recreated. Run `lerd start` to bring your sites back up.")
	return nil
}
