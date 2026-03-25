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

// NewPhpExtCmd returns the php:ext parent command.
func NewPhpExtCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "php:ext",
		Short: "Manage custom PHP extensions",
	}
	cmd.AddCommand(newPhpExtAddCmd())
	cmd.AddCommand(newPhpExtRemoveCmd())
	cmd.AddCommand(newPhpExtListCmd())
	return cmd
}

func newPhpExtAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <ext> [version]",
		Short: "Install a custom PHP extension (rebuilds the FPM image)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			ext := args[0]
			version, err := phpExtVersion(args[1:])
			if err != nil {
				return err
			}

			cfg, err := config.LoadGlobal()
			if err != nil {
				return err
			}

			cfg.AddExtension(version, ext)
			if err := config.SaveGlobal(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			fmt.Printf("Adding extension %q to PHP %s image...\n", ext, version)
			if err := podman.RebuildFPMImage(version); err != nil {
				return err
			}

			short := strings.ReplaceAll(version, ".", "")
			unit := "lerd-php" + short + "-fpm"
			if err := services.Mgr.Restart(unit); err != nil {
				fmt.Printf("[WARN] restart %s: %v\n", unit, err)
				fmt.Printf("Run: systemctl --user restart %s\n", unit)
			} else {
				fmt.Printf("FPM container restarted.\n")
			}

			fmt.Printf("Extension %q installed for PHP %s.\n", ext, version)
			return nil
		},
	}
}

func newPhpExtRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <ext> [version]",
		Short: "Remove a custom PHP extension (rebuilds the FPM image)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			ext := args[0]
			version, err := phpExtVersion(args[1:])
			if err != nil {
				return err
			}

			cfg, err := config.LoadGlobal()
			if err != nil {
				return err
			}

			cfg.RemoveExtension(version, ext)
			if err := config.SaveGlobal(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			fmt.Printf("Removing extension %q from PHP %s image...\n", ext, version)
			if err := podman.RebuildFPMImage(version); err != nil {
				return err
			}

			short := strings.ReplaceAll(version, ".", "")
			unit := "lerd-php" + short + "-fpm"
			if err := services.Mgr.Restart(unit); err != nil {
				fmt.Printf("[WARN] restart %s: %v\n", unit, err)
				fmt.Printf("Run: systemctl --user restart %s\n", unit)
			} else {
				fmt.Printf("FPM container restarted.\n")
			}

			fmt.Printf("Extension %q removed for PHP %s.\n", ext, version)
			return nil
		},
	}
}

func newPhpExtListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [version]",
		Short: "List custom PHP extensions for a version",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			version, err := phpExtVersion(args)
			if err != nil {
				return err
			}

			cfg, err := config.LoadGlobal()
			if err != nil {
				return err
			}

			exts := cfg.GetExtensions(version)
			if len(exts) == 0 {
				fmt.Printf("No custom extensions configured for PHP %s.\n", version)
				return nil
			}

			fmt.Printf("Custom extensions for PHP %s:\n", version)
			for _, ext := range exts {
				fmt.Printf("  - %s\n", ext)
			}
			return nil
		},
	}
}

// phpExtVersion resolves the PHP version from args, cwd detection, or global default.
func phpExtVersion(args []string) (string, error) {
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
