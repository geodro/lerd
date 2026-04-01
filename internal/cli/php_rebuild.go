package cli

import (
	"fmt"
	"io"
	"strings"

	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
	"github.com/spf13/cobra"
)

// NewPhpRebuildCmd returns the php:rebuild command.
func NewPhpRebuildCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "php:rebuild [version]",
		Short: "Force-rebuild PHP-FPM image(s)",
		Long:  "Force-rebuilds lerd PHP-FPM container images. Pulls a pre-built base from ghcr.io by default; pass --local to build entirely from source.\nPass a version (e.g. 8.3) to rebuild only that version, or omit to rebuild all installed versions.",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runPhpRebuild,
	}
	cmd.Flags().Bool("local", false, "Build images locally instead of pulling pre-built base images")
	return cmd
}

func runPhpRebuild(cmd *cobra.Command, args []string) error {
	local, _ := cmd.Flags().GetBool("local")
	var versions []string

	if len(args) == 1 {
		versions = []string{args[0]}
	} else {
		var err error
		versions, err = phpPkg.ListInstalled()
		if err != nil {
			return fmt.Errorf("listing PHP versions: %w", err)
		}
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
			Run:   func(w io.Writer) error { return podman.RebuildFPMImageTo(ver, local, w) },
		}
	}
	RunParallel(jobs) //nolint:errcheck — individual failures printed by RunParallel

	// Store the new Containerfile hash so future updates know images are current.
	if err := podman.StoreFPMHash(); err != nil {
		fmt.Printf("  [WARN] could not store image hash: %v\n", err)
	}

	label := "PHP-FPM images"
	if len(versions) == 1 {
		label = "PHP " + versions[0] + " image"
	}
	fmt.Printf("\n%s rebuilt. Restarting containers...\n", label)
	for _, v := range versions {
		unit := "lerd-php" + strings.ReplaceAll(v, ".", "") + "-fpm"
		if err := services.Mgr.Restart(unit); err != nil {
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
