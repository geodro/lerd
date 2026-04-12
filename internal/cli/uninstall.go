package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
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

	// Ask about data removal up front — the StepRunner puts stdin into raw
	// mode and its reader goroutine would consume bytes meant for this prompt.
	removeData := force || confirmRemoveData()

	// DNS teardown runs outside the step runner because it may prompt for sudo.
	fmt.Println("  --> Removing DNS configuration")
	dns.Teardown()

	step("Stopping containers and services")
	{
		// Use the service manager so this works on both Linux (systemd/quadlet)
		// and macOS (launchd plists).
		seen := map[string]bool{}
		for _, unit := range services.Mgr.ListContainerUnits("lerd-*") {
			seen[unit] = true
			status, _ := podman.UnitStatus(unit)
			if status == "active" || status == "activating" {
				_ = podman.StopUnit(unit)
			}
			_ = services.Mgr.Disable(unit)
		}
		for _, unit := range services.Mgr.ListServiceUnits("lerd-*") {
			if seen[unit] {
				continue
			}
			status, _ := podman.UnitStatus(unit)
			if status == "active" || status == "activating" {
				_ = podman.StopUnit(unit)
			}
			_ = services.Mgr.Disable(unit)
		}
		for _, unit := range services.Mgr.ListTimerUnits("lerd-*") {
			_ = podman.StopUnit(unit)
			_ = services.Mgr.Disable(unit)
		}
		// Kill any running tray process. The tray may be running standalone
		// (launched from the desktop file or `lerd tray`) without a unit,
		// in which case the unit teardown above misses it.
		killTray()
	}
	ok()

	step("Removing service units")
	{
		seen := map[string]bool{}
		for _, unit := range services.Mgr.ListContainerUnits("lerd-*") {
			seen[unit] = true
			_ = services.Mgr.RemoveContainerUnit(unit)
		}
		for _, unit := range services.Mgr.ListServiceUnits("lerd-*") {
			if seen[unit] {
				continue
			}
			_ = services.Mgr.RemoveServiceUnit(unit)
		}
		for _, unit := range services.Mgr.ListTimerUnits("lerd-*") {
			_ = services.Mgr.RemoveTimerUnit(strings.TrimSuffix(unit, ".timer"))
		}
	}
	ok()

	step("Reloading service manager")
	_ = podman.DaemonReloadFn()
	ok()

	step("Removing lerd Podman network")
	_ = podman.RunSilent("network", "rm", "lerd")
	ok()

	step("Removing shell PATH entry")
	removeShellEntry()
	ok()

	step("Removing lerd binary")
	if self, err := selfPath(); err == nil {
		os.Remove(self) //nolint:errcheck
	}
	ok()

	fmt.Println()
	if removeData {
		fmt.Print("  --> Removing config and data directories ... ")
		os.RemoveAll(config.ConfigDir())
		os.RemoveAll(config.DataDir())
		fmt.Println("OK")
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
