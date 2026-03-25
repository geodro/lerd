package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/geodro/lerd/internal/config"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
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
	return &cobra.Command{
		Use:   "on [version]",
		Short: "Enable Xdebug for a PHP version (rebuilds the FPM image)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runXdebugToggle(args, true)
		},
	}
}

func newXdebugOffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "off [version]",
		Short: "Disable Xdebug for a PHP version (rebuilds the FPM image)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runXdebugToggle(args, false)
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

func runXdebugToggle(args []string, enable bool) error {
	version, err := xdebugVersion(args)
	if err != nil {
		return err
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}

	// No-op if already in the desired state.
	if cfg.IsXdebugEnabled(version) == enable {
		state := "disabled"
		if enable {
			state = "enabled"
		}
		fmt.Printf("Xdebug is already %s for PHP %s\n", state, version)
		return nil
	}

	cfg.SetXdebug(version, enable)
	if err := config.SaveGlobal(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	if err := podman.WriteXdebugIni(version, enable); err != nil {
		return fmt.Errorf("writing xdebug ini: %w", err)
	}

	// Update quadlet (adds volume mount if not already present) + restart.
	if err := podman.WriteFPMQuadlet(version); err != nil {
		fmt.Printf("[WARN] updating quadlet: %v\n", err)
	}

	short := strings.ReplaceAll(version, ".", "")
	unit := "lerd-php" + short + "-fpm"
	if err := services.Mgr.Restart(unit); err != nil {
		fmt.Printf("[WARN] restart %s: %v\n", unit, err)
		fmt.Printf("Run: systemctl --user restart %s\n", unit)
	} else {
		fmt.Printf("FPM container restarted.\n")
	}

	state := "disabled"
	if enable {
		state = "enabled"
	}
	fmt.Printf("Xdebug %s for PHP %s (port 9003, host.containers.internal)\n", state, version)
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

	fmt.Printf("%-10s %s\n", "Version", "Xdebug")
	fmt.Printf("%-10s %s\n", "─────────", "──────")
	for _, v := range versions {
		status := "\033[33mdisabled\033[0m"
		if cfg.IsXdebugEnabled(v) {
			status = "\033[32menabled\033[0m"
		}
		fmt.Printf("%-10s %s\n", v, status)
	}
	return nil
}
