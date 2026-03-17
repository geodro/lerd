package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
)

// NewUninstallCmd returns the uninstall command.
func NewUninstallCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove Lerd and all its components",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runUninstall(force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompts")
	return cmd
}

func runUninstall(force bool) error {
	fmt.Println("==> Uninstalling Lerd")

	if !force {
		fmt.Print("  This will stop all containers and remove Lerd. Continue? [y/N] ")
		if !readYes() {
			fmt.Println("  Aborted.")
			return nil
		}
	}

	// 1. Discover all installed units from quadlet files on disk, plus the watcher service.
	quadletDir := config.QuadletDir()
	var units []string
	if entries, err := filepath.Glob(filepath.Join(quadletDir, "lerd-*.container")); err == nil {
		for _, f := range entries {
			// lerd-foo.container → lerd-foo
			units = append(units, strings.TrimSuffix(filepath.Base(f), ".container"))
		}
	}
	// Always include the watcher and UI services even if they have no quadlet file.
	units = append(units, "lerd-watcher", "lerd-ui")

	step("Stopping containers and services")
	for _, unit := range units {
		status, _ := podman.UnitStatus(unit)
		if status == "active" {
			_ = podman.StopUnit(unit)
		}
		// disable so systemd forgets about them after quadlet files are removed
		_ = disableUnit(unit)
	}
	ok()

	// 2. Remove Quadlet files
	step("Removing Quadlet units")
	if entries, err := filepath.Glob(filepath.Join(quadletDir, "lerd-*.container")); err == nil {
		for _, f := range entries {
			os.Remove(f) //nolint:errcheck
		}
	}
	ok()

	// 3. Remove user service unit files
	step("Removing systemd user services")
	for _, svc := range []string{"lerd-watcher.service", "lerd-ui.service"} {
		os.Remove(filepath.Join(config.SystemdUserDir(), svc)) //nolint:errcheck
	}
	ok()

	// 4. daemon-reload so systemd clears its state
	step("Reloading systemd daemon")
	_ = podman.DaemonReload()
	ok()

	// 5. Remove Podman network (best-effort)
	step("Removing lerd Podman network")
	_ = podman.RunSilent("network", "rm", "lerd")
	ok()

	// 6. Remove shell PATH entry
	step("Removing shell PATH entry")
	removeShellEntry()
	ok()

	// 7. Remove binary
	step("Removing lerd binary")
	self, err := selfPath()
	if err == nil {
		os.Remove(self)
	}
	ok()

	// 8. Optionally remove data and config
	fmt.Println()
	if force || confirmRemoveData() {
		step("Removing config and data directories")
		os.RemoveAll(config.ConfigDir())
		os.RemoveAll(config.DataDir())
		ok()
	} else {
		fmt.Printf("  Config kept at %s\n", config.ConfigDir())
		fmt.Printf("  Data kept at   %s\n", config.DataDir())
	}

	fmt.Println("\nLerd uninstalled.")
	return nil
}

func confirmRemoveData() bool {
	fmt.Print("  Remove all config and data (~/.config/lerd, ~/.local/share/lerd)? [y/N] ")
	return readYes()
}

func readYes() bool {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	ans := strings.TrimSpace(scanner.Text())
	return strings.EqualFold(ans, "y") || strings.EqualFold(ans, "yes")
}

func disableUnit(name string) error {
	return runSystemctlUser("disable", name)
}

func removeShellEntry() {
	const marker = "# Added by Lerd installer"
	home, _ := os.UserHomeDir()

	candidates := []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".config", "fish", "conf.d", "lerd.fish"),
	}

	for _, rc := range candidates {
		removeMarkedBlock(rc, marker)
	}
}

func runSystemctlUser(args ...string) error {
	cmd := exec.Command("systemctl", append([]string{"--user"}, args...)...)
	return cmd.Run()
}

// removeMarkedBlock removes the marker line and the line immediately after it.
func removeMarkedBlock(path, marker string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	lines := strings.Split(string(data), "\n")
	out := make([]string, 0, len(lines))
	skip := 0
	for _, line := range lines {
		if skip > 0 {
			skip--
			continue
		}
		if strings.TrimSpace(line) == marker {
			skip = 1 // also skip the next line (the PATH export)
			continue
		}
		out = append(out, line)
	}

	// Only rewrite if something changed
	result := strings.Join(out, "\n")
	if result != string(data) {
		os.WriteFile(path, []byte(result), 0644) //nolint:errcheck
	}
}
