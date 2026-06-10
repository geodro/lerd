package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/geodro/lerd/internal/config"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
)

// NewPhpPkgCmd returns the php:pkg parent command, which manages extra Alpine
// packages baked into the PHP-FPM image's runtime stage. Unlike php:bun (a
// runtime install into a volume), these are layered into the image at build
// time and re-applied on every rebuild, like php:ext.
func NewPhpPkgCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "php:pkg",
		Short: "Manage extra Alpine packages in the PHP-FPM image",
	}
	cmd.AddCommand(newPhpPkgAddCmd())
	cmd.AddCommand(newPhpPkgRemoveCmd())
	cmd.AddCommand(newPhpPkgListCmd())
	return cmd
}

// phpPkgVersion resolves the PHP version from the --php flag, cwd detection, or
// the global default.
func phpPkgVersion(flagVer string) (string, error) {
	if flagVer != "" {
		return flagVer, nil
	}
	if cwd, err := os.Getwd(); err == nil {
		if v, err := phpDet.DetectVersion(cwd); err == nil {
			return v, nil
		}
	}
	cfg, err := config.LoadGlobal()
	if err != nil {
		return "", err
	}
	return cfg.PHP.DefaultVersion, nil
}

func newPhpPkgAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <package...>",
		Short: "Add Alpine packages to the FPM image and rebuild",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pkgs, err := podman.ParseApkDeps(strings.Join(args, " "))
			if err != nil {
				return err
			}
			flagVer, _ := cmd.Flags().GetString("php")
			version, err := phpPkgVersion(flagVer)
			if err != nil {
				return err
			}

			cfg, err := config.LoadGlobal()
			if err != nil {
				return err
			}
			for _, p := range pkgs {
				cfg.AddPackage(version, p)
			}
			if err := config.SaveGlobal(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			fmt.Printf("Adding packages to PHP %s image: %s\n", version, strings.Join(pkgs, " "))
			if err := podman.RebuildFPMImage(version, false); err != nil {
				// A bad package name fails the build; revert so a broken entry
				// doesn't linger in config and poison future rebuilds.
				for _, p := range pkgs {
					cfg.RemovePackage(version, p)
				}
				if saveErr := config.SaveGlobal(cfg); saveErr != nil {
					fmt.Printf("[WARN] reverting config: %v\n", saveErr)
				}
				return fmt.Errorf("rebuild failed (config reverted): %w", err)
			}

			restartFPMUnit(version)
			fmt.Printf("Packages installed for PHP %s.\n", version)
			return nil
		},
	}
	cmd.Flags().String("php", "", "PHP version (defaults to the current project or global default)")
	return cmd
}

func newPhpPkgRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <package...>",
		Short: "Remove extra Alpine packages and rebuild",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flagVer, _ := cmd.Flags().GetString("php")
			version, err := phpPkgVersion(flagVer)
			if err != nil {
				return err
			}
			cfg, err := config.LoadGlobal()
			if err != nil {
				return err
			}
			for _, p := range args {
				cfg.RemovePackage(version, p)
			}
			if err := config.SaveGlobal(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			fmt.Printf("Removing packages from PHP %s image: %s\n", version, strings.Join(args, " "))
			if err := podman.RebuildFPMImage(version, false); err != nil {
				return err
			}
			restartFPMUnit(version)
			fmt.Printf("Packages removed for PHP %s.\n", version)
			return nil
		},
	}
	cmd.Flags().String("php", "", "PHP version (defaults to the current project or global default)")
	return cmd
}

func newPhpPkgListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List extra Alpine packages configured for a PHP version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			flagVer, _ := cmd.Flags().GetString("php")
			version, err := phpPkgVersion(flagVer)
			if err != nil {
				return err
			}
			cfg, err := config.LoadGlobal()
			if err != nil {
				return err
			}
			pkgs := cfg.GetPackages(version)
			if len(pkgs) == 0 {
				fmt.Printf("No extra packages configured for PHP %s.\n", version)
				return nil
			}
			fmt.Printf("Extra packages for PHP %s:\n", version)
			for _, p := range pkgs {
				fmt.Printf("  - %s\n", p)
			}
			return nil
		},
	}
	cmd.Flags().String("php", "", "PHP version (defaults to the current project or global default)")
	return cmd
}

// restartFPMUnit restarts the FPM container for a PHP version after an image
// rebuild, printing a manual hint if the restart fails.
func restartFPMUnit(version string) {
	unit := "lerd-php" + strings.ReplaceAll(version, ".", "") + "-fpm"
	if err := podman.RestartUnit(unit); err != nil {
		fmt.Printf("[WARN] restart %s: %v\n", unit, err)
		fmt.Printf("Run: systemctl --user restart %s\n", unit)
	} else {
		fmt.Printf("FPM container restarted.\n")
	}
}
