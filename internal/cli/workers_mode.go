package cli

import (
	"fmt"
	"runtime"

	"github.com/geodro/lerd/internal/config"
	"github.com/spf13/cobra"
)

// NewWorkersCmd returns the `lerd workers` parent command. Currently only
// `lerd workers mode [exec|container]` lives here, but the subcommand is
// structured as a group so future runtime-level options (concurrency,
// restart delay, ...) have an obvious home.
func NewWorkersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workers",
		Short: "Manage the worker runtime configuration (macOS-only for now)",
	}
	cmd.AddCommand(newWorkersModeCmd())
	return cmd
}

func newWorkersModeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mode [exec|container]",
		Short: "Show or set how framework workers are launched on macOS",
		Long: `Show or set the macOS worker runtime mode.

  exec       one podman exec per worker, supervised by launchd with a pid-file
             dedup guard. Lower memory; all workers share the FPM container's
             PHP process and OPcache. Default.

  container  one detached container per worker spawned from the FPM image.
             Higher memory; 1:1 supervisor boundary, more robust against
             podman-machine SSH bridge hiccups.

No argument prints the current value. The setting is ignored on Linux
which always uses exec-mode workers under systemd.

Changing the mode on macOS stops each active worker in its old shape,
cleans up the stale on-disk artifacts, and restarts it in the new shape.
No manual 'lerd stop && lerd start' needed.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			mode, show, err := workersModeFromArgs(args)
			if err != nil {
				return err
			}
			cfg, err := config.LoadGlobal()
			if err != nil {
				return err
			}
			if show {
				return printWorkersMode(cfg)
			}
			prev := cfg.WorkerExecMode()
			if err := applyWorkersMode(mode); err != nil {
				return err
			}
			if prev == mode {
				fmt.Printf("Worker mode already %s.\n", mode)
				return nil
			}
			fmt.Printf("Worker mode set to %s (was %s).\n", mode, prev)
			if runtime.GOOS == "darwin" {
				fmt.Println("Active workers have been restarted in the new shape.")
			} else {
				fmt.Println("Note: Linux always uses the exec runtime. This setting only applies on macOS.")
			}
			return nil
		},
	}
}

// workersModeFromArgs parses the user's `workers mode ...` argv. Returns
// (mode, show, err): `show` true means "no argument, print current".
func workersModeFromArgs(args []string) (mode string, show bool, err error) {
	if len(args) == 0 {
		return "", true, nil
	}
	switch args[0] {
	case config.WorkerExecModeExec, config.WorkerExecModeContainer:
		return args[0], false, nil
	}
	return "", false, fmt.Errorf("unknown mode %q, expected %q or %q",
		args[0], config.WorkerExecModeExec, config.WorkerExecModeContainer)
}

// applyWorkersMode writes newMode to global config, then (on macOS, if
// the mode actually changed) stops every active worker in its old shape,
// removes stale on-disk artifacts, and restarts each in the new shape.
// On Linux it's a pure config write since workers always use exec under
// systemd. Idempotent for same-value writes.
func applyWorkersMode(newMode string) error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	prev := cfg.WorkerExecMode()
	cfg.Workers.ExecMode = newMode
	if err := config.SaveGlobal(cfg); err != nil {
		return err
	}
	// migrateWorkersOnModeChange is a no-op on Linux (build-tag linked).
	// On macOS it executes the stop → clean → restart dance per worker.
	return migrateWorkersOnModeChange(prev, newMode)
}

func printWorkersMode(cfg *config.GlobalConfig) error {
	fmt.Printf("Worker mode: %s\n", cfg.WorkerExecMode())
	if runtime.GOOS != "darwin" {
		fmt.Println("  (Linux runs workers via podman exec under systemd; setting is informational.)")
	}
	return nil
}
