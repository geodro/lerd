package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

	// Ask about data removal up front before DNS teardown, which may prompt for sudo.
	removeData := force || confirmRemoveData()

	// DNS teardown runs outside the step runner because it may prompt for sudo.
	fmt.Println("  --> Removing DNS configuration")
	dns.Teardown()

	step("Stopping containers and services")
	{
		// Collect all lerd-* units from both container and service managers.
		seen := map[string]bool{}
		var units []string
		for _, u := range services.Mgr.ListContainerUnits("lerd-*") {
			if !seen[u] {
				seen[u] = true
				units = append(units, u)
			}
		}
		for _, u := range services.Mgr.ListServiceUnits("lerd-*") {
			if !seen[u] {
				seen[u] = true
				units = append(units, u)
			}
		}
		for _, unit := range units {
			status, _ := services.Mgr.UnitStatus(unit)
			if status == "active" {
				_ = services.Mgr.Stop(unit)
			}
			_ = services.Mgr.Disable(unit)
		}
	}
	ok()

	step("Removing units and service files")
	for _, unit := range services.Mgr.ListContainerUnits("lerd-*") {
		services.Mgr.RemoveContainerUnit(unit) //nolint:errcheck
	}
	for _, unit := range services.Mgr.ListServiceUnits("lerd-*") {
		services.Mgr.RemoveServiceUnit(unit) //nolint:errcheck
	}
	ok()

	step("Reloading service manager")
	_ = services.Mgr.DaemonReload()
	ok()

	step("Removing lerd Podman network")
	_ = podman.RunSilent("network", "rm", "lerd")
	ok()

	if runtime.GOOS != "darwin" {
		step("Removing shell PATH entry")
		removeShellEntry()
		ok()

		step("Removing lerd binary")
		if self, err := selfPath(); err == nil {
			os.Remove(self) //nolint:errcheck
		}
		ok()
	}

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

	if runtime.GOOS == "darwin" {
		fmt.Println("\n  To remove the lerd binary, run:")
		fmt.Println("    brew uninstall lerd")
		fmt.Println()
		fmt.Println("  Tip: if you ever run `brew uninstall` first, clean up with:")
		fmt.Println("    ~/.local/bin/lerd-cleanup")
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
