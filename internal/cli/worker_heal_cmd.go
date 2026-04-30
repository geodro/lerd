package cli

import (
	"fmt"
	"os"

	"github.com/geodro/lerd/internal/config"
	"github.com/spf13/cobra"
)

// newWorkerHealCmd returns the `lerd worker heal [name]` command. Heal is a
// runtime recovery operation: it never writes to .lerd.yaml or rewrites the
// worker's unit file. With no argument, it scans every registered site and
// heals any worker whose unit is in the systemd "failed" state. With a
// worker name, it heals that worker for the site at the current working
// directory.
func newWorkerHealCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "heal [name]",
		Short: "Reset failed worker units and start them again",
		Long: `Heal worker units stuck in systemd's "failed" state.

Workers that crash and exhaust their Restart= rate limit land in "failed"
and stay there until something resets them. This command finds those
units (across every registered site by default, or the named worker for
the current site if an argument is given) and runs the equivalent of
` + "`systemctl --user reset-failed UNIT && systemctl --user start UNIT`" + `
for each one.

Heal is intentionally surgical:
  - It does NOT rewrite the unit file from .lerd.yaml.
  - It does NOT change the workers list in .lerd.yaml.
  - It does NOT touch sites the user has paused.

Worker enable/disable belongs to 'lerd worker start/stop/add/remove'.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 1 {
				return runHealOne(args[0])
			}
			return runHealAll()
		},
	}
}

func runHealAll() error {
	report, err := HealWorkers(func(evt HealEvent) {
		switch evt.Phase {
		case "starting":
			fmt.Printf("  ➜  %s ", evt.Unit)
		case "healed":
			fmt.Println("OK")
		case "failed":
			if evt.Unit != "" {
				fmt.Printf("FAIL (%s)\n", evt.Error)
			}
		}
	})
	if err != nil {
		return err
	}
	fmt.Println(HealSummary(report))
	return nil
}

func runHealOne(workerName string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	site, err := config.FindSiteByPath(cwd)
	if err != nil {
		return fmt.Errorf("not a registered site — run 'lerd link' first")
	}
	unit := "lerd-" + workerName + "-" + site.Name
	fmt.Printf("  ➜  %s ", unit)
	if err := HealUnit(unit); err != nil {
		fmt.Printf("FAIL (%v)\n", err)
		return err
	}
	fmt.Println("OK")
	return nil
}
