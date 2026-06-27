package cli

import (
	"fmt"

	"github.com/geodro/lerd/internal/cleanup"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/feedback"
	"github.com/spf13/cobra"
)

// NewCleanupCmd returns the cleanup command: reclaim podman disk that lerd's
// own image rebuilds have orphaned, without ever touching a non-lerd resource.
func NewCleanupCmd() *cobra.Command {
	var dryRun, yes, deep bool
	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Reclaim podman disk from orphaned lerd images",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runCleanup(dryRun, yes, deep)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be reclaimed without removing anything")
	cmd.Flags().BoolVar(&yes, "yes", false, "Remove without confirming")
	cmd.Flags().BoolVar(&deep, "deep", false, "Also remove unused service images no service references any more")
	cmd.AddCommand(newCleanupAutoCmd())
	return cmd
}

// newCleanupAutoCmd toggles automatic cleanup (the watcher's daily safe-tier
// sweep and the post-rebuild / post-service-change reaping), so users don't have
// to hand-edit config.yaml. Matches the on/off/status shape of lerd idle/notify.
func newCleanupAutoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auto",
		Short: "Enable, disable, or show automatic cleanup",
		Long: `Toggle automatic cleanup: the lerd-watcher's daily safe-tier sweep and
the immediate reaping after a PHP rebuild or a service update/remove. On by
default. Only ever runs the safe tier, never --deep.`,
	}
	cmd.AddCommand(
		&cobra.Command{Use: "on", Short: "Enable automatic cleanup", Args: cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error { return runCleanupAutoToggle(true) }},
		&cobra.Command{Use: "off", Short: "Disable automatic cleanup", Args: cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error { return runCleanupAutoToggle(false) }},
		&cobra.Command{Use: "status", Short: "Show whether automatic cleanup is on", Args: cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error { return runCleanupAutoStatus() }},
	)
	return cmd
}

func runCleanupAutoToggle(enable bool) error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if cfg.AutoCleanupEnabled() == enable {
		feedback.Line(fmt.Sprintf("Automatic cleanup already %s.", autoStateWord(enable)))
		return nil
	}
	cfg.AutoCleanup = enable
	if err := config.SaveGlobal(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	feedback.Done(fmt.Sprintf("Automatic cleanup %s.", autoStateWord(enable)))
	return nil
}

func runCleanupAutoStatus() error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	state := feedback.Amber("disabled")
	if cfg.AutoCleanupEnabled() {
		state = feedback.Green("enabled")
	}
	fmt.Printf("Automatic cleanup: %s\n", state)
	return nil
}

func autoStateWord(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func runCleanup(dryRun, yes, deep bool) error {
	plan, err := cleanup.Inspect(deep)
	if err != nil {
		return err
	}
	if len(plan.Targets) == 0 {
		feedback.Line("Nothing to clean up. No orphaned lerd images.")
		return nil
	}

	// Sizes are per-image reclaimable (unique layers only); shared base layers
	// stay behind for the live images that still reference them.
	rows := make([][]string, 0, len(plan.Targets))
	for _, t := range plan.Targets {
		rows = append(rows, []string{t.ID, t.Desc, humanSize(t.Bytes)})
	}
	feedback.Header("Reclaimable lerd images")
	feedback.Table([]string{"IMAGE", "KIND", "RECLAIMABLE"}, rows)
	feedback.Note(fmt.Sprintf("About %s across %d image(s).", humanSize(plan.ReclaimBytes()), len(plan.Targets)))

	if dryRun {
		return nil
	}
	if !yes && !feedback.Confirm("Remove these images?", false) {
		return nil
	}

	feedback.Done(fmt.Sprintf("Freed about %s.", humanSize(cleanup.Apply(plan))))
	return nil
}
