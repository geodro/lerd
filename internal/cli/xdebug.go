package cli

import (
	"fmt"
	"os"

	"github.com/geodro/lerd/internal/config"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/xdebugops"
	"github.com/spf13/cobra"
)

// NewXdebugCmd returns the xdebug parent command with on/off/status subcommands.
func NewXdebugCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "xdebug",
		Short: "Toggle Xdebug for a PHP version",
	}
	cmd.AddCommand(newXdebugOnCmd())
	cmd.AddCommand(newXdebugOffCmd())
	cmd.AddCommand(newXdebugStatusCmd())
	return cmd
}

func newXdebugOnCmd() *cobra.Command {
	var mode string
	cmd := &cobra.Command{
		Use:   "on [version]",
		Short: "Enable Xdebug for a PHP version (rebuilds the FPM image)",
		Long:  "Enable Xdebug for a PHP version. Use --mode to pick a non-default mode, e.g. --mode coverage for code coverage, or --mode debug,coverage to combine.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			normalised, err := podman.NormaliseXdebugMode(mode)
			if err != nil {
				return err
			}
			return runXdebugToggle(args, true, normalised)
		},
	}
	cmd.Flags().StringVar(&mode, "mode", "debug", "xdebug.mode value (debug, coverage, develop, profile, trace, gcstats, or a comma-separated combo)")
	return cmd
}

func newXdebugOffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "off [version]",
		Short: "Disable Xdebug for a PHP version (rebuilds the FPM image)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runXdebugToggle(args, false, "")
		},
	}
}

func newXdebugStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show Xdebug status for all installed PHP versions",
		RunE:  runXdebugStatus,
	}
}

func xdebugVersion(args []string) (string, error) {
	if len(args) == 1 {
		return args[0], nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	v, err := phpDet.DetectVersion(cwd)
	if err != nil {
		cfg, err := config.LoadGlobal()
		if err != nil {
			return "", err
		}
		return cfg.PHP.DefaultVersion, nil
	}
	return v, nil
}

func runXdebugToggle(args []string, enable bool, mode string) error {
	version, err := xdebugVersion(args)
	if err != nil {
		return err
	}

	applyMode := ""
	if enable {
		applyMode = mode
	}

	res, err := xdebugops.Apply(version, applyMode)
	if err != nil {
		return err
	}

	if res.NoChange {
		state := "disabled"
		if res.Enabled {
			state = fmt.Sprintf("enabled (mode=%s)", res.Mode)
		}
		fmt.Printf("Xdebug is already %s for PHP %s\n", state, version)
		return nil
	}

	if res.RestartErr != nil {
		unit := xdebugops.FPMUnit(version)
		fmt.Printf("[WARN] restart %s: %v\n", unit, res.RestartErr)
		fmt.Printf("Run: systemctl --user restart %s\n", unit)
	} else if res.Restarted {
		fmt.Printf("FPM container restarted.\n")
	}

	if res.Enabled {
		fmt.Printf("Xdebug enabled for PHP %s (mode=%s, port 9003, host.containers.internal)\n", version, res.Mode)
	} else {
		fmt.Printf("Xdebug disabled for PHP %s\n", version)
	}
	return nil
}

func runXdebugStatus(_ *cobra.Command, _ []string) error {
	versions, err := phpDet.ListInstalled()
	if err != nil {
		return fmt.Errorf("listing PHP versions: %w", err)
	}

	if len(versions) == 0 {
		fmt.Println("No PHP versions installed.")
		return nil
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}

	fmt.Printf("%-10s %-10s %s\n", "Version", "Xdebug", "Mode")
	fmt.Printf("%-10s %-10s %s\n", "─────────", "──────────", "────")
	for _, v := range versions {
		state := "disabled"
		color := "\033[33m"
		mode := cfg.GetXdebugMode(v)
		if mode != "" {
			state = "enabled"
			color = "\033[32m"
		}
		fmt.Printf("%-10s %s%-10s\033[0m %s\n", v, color, state, mode)
	}
	return nil
}
