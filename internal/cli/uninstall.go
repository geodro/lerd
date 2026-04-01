package cli

import (
	"bufio"
	"fmt"
	"io"
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

	// DNS teardown and all stdin reads must happen before NewStepRunner().
	// StepRunner puts the terminal in raw mode and its keyreader goroutine
	// consumes stdin, breaking sudo prompts and bufio reads after r.Close().
	fmt.Print("  --> Removing DNS configuration ... ")
	dns.Teardown()
	fmt.Println("OK")

	// Ask about data removal now, before raw mode starts.
	fmt.Println()
	removeData := force || confirmRemoveData()
	fmt.Println()

	r := NewStepRunner()

	r.Run("Stopping containers and services", func(_ io.Writer) error { //nolint:errcheck
		units := services.Mgr.ListContainerUnits("lerd-*")
		units = append(units, "lerd-watcher", "lerd-ui")
		for _, unit := range units {
			status, _ := services.Mgr.UnitStatus(unit)
			if status == "active" {
				_ = services.Mgr.Stop(unit)
			}
			_ = services.Mgr.Disable(unit)
		}
		return nil
	})

	r.Run("Removing container units", func(_ io.Writer) error { //nolint:errcheck
		for _, unit := range services.Mgr.ListContainerUnits("lerd-*") {
			services.Mgr.RemoveContainerUnit(unit) //nolint:errcheck
		}
		return nil
	})

	r.Run("Removing service files", func(_ io.Writer) error { //nolint:errcheck
		for _, name := range []string{"lerd-watcher", "lerd-ui"} {
			services.Mgr.RemoveServiceUnit(name) //nolint:errcheck
		}
		return nil
	})

	r.Run("Reloading service manager", func(_ io.Writer) error { //nolint:errcheck
		_ = services.Mgr.DaemonReload()
		return nil
	})

	r.Run("Removing lerd Podman network", func(_ io.Writer) error { //nolint:errcheck
		_ = podman.RunSilent("network", "rm", "lerd")
		return nil
	})

	if runtime.GOOS != "darwin" {
		r.Run("Removing shell PATH entry", func(_ io.Writer) error { //nolint:errcheck
			removeShellEntry()
			return nil
		})

		r.Run("Removing lerd binary", func(_ io.Writer) error { //nolint:errcheck
			if self, err := selfPath(); err == nil {
				os.Remove(self) //nolint:errcheck
			}
			return nil
		})
	}

	r.Close()

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
