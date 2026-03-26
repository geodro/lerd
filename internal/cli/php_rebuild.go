package cli

import (
	"fmt"
	"io"
	"strings"

	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
	"github.com/spf13/cobra"
)

// NewPhpRebuildCmd returns the php:rebuild command.
func NewPhpRebuildCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "php:rebuild",
		Short: "Force-rebuild all installed PHP-FPM images",
		Long:  "Removes and rebuilds all lerd PHP-FPM container images. Run after a lerd update to pick up Containerfile changes.",
		RunE:  runPhpRebuild,
	}
}

func runPhpRebuild(_ *cobra.Command, _ []string) error {
	versions, err := phpPkg.ListInstalled()
	if err != nil {
		return fmt.Errorf("listing PHP versions: %w", err)
	}

	if len(versions) == 0 {
		fmt.Println("No PHP versions installed.")
		return nil
	}

	jobs := make([]BuildJob, len(versions))
	for i, v := range versions {
		ver := v
		jobs[i] = BuildJob{
			Label: "PHP " + ver,
			Run:   func(w io.Writer) error { return podman.RebuildFPMImageTo(ver, w) },
		}
	}
	RunParallel(jobs) //nolint:errcheck — individual failures printed by RunParallel

	// Store the new Containerfile hash so future updates know images are current.
	if err := podman.StoreFPMHash(); err != nil {
		fmt.Printf("  [WARN] could not store image hash: %v\n", err)
	}

	fmt.Println("\nAll PHP-FPM images rebuilt. Restarting containers...")
	for _, v := range versions {
		unit := "lerd-php" + strings.ReplaceAll(v, ".", "") + "-fpm"
		if err := podman.RestartUnit(unit); err != nil {
			fmt.Printf("  [WARN] restart %s: %v\n", unit, err)
		} else {
			fmt.Printf("  restarted %s\n", unit)
		}
	}

	// Restart workers that run inside FPM containers via podman exec.
	// BindsTo stops them when the FPM container stops but does not restart
	// them when it comes back up, so we do it explicitly here.
	for _, unit := range append(append(registeredReverbUnits(), registeredQueueUnits()...), registeredScheduleUnits()...) {
		if lerdSystemd.IsServiceActive(unit) || lerdSystemd.IsServiceEnabled(unit) {
			if err := lerdSystemd.RestartService(unit); err != nil {
				fmt.Printf("  [WARN] restart %s: %v\n", unit, err)
			} else {
				fmt.Printf("  restarted %s\n", unit)
			}
		}
	}

	return nil
}
