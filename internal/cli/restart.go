package cli

import (
	"fmt"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
)

// NewRestartCmd returns the restart command.
func NewRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart [site]",
		Short: "Restart the container for the current or named site",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name, err := resolveSiteName(args)
			if err != nil {
				return err
			}
			return RestartSite(name)
		},
	}
}

// RestartSite restarts the custom container for a site. For PHP sites
// it restarts the shared FPM container for that site's PHP version.
func RestartSite(name string) error {
	site, err := config.FindSite(name)
	if err != nil {
		return fmt.Errorf("site %q not found", name)
	}

	if site.IsCustomContainer() {
		unit := podman.CustomContainerName(site.Name)
		if err := podman.RestartUnit(unit); err != nil {
			return fmt.Errorf("restarting container: %w", err)
		}
		fmt.Printf("Restarted: %s (%s)\n", name, unit)
		return nil
	}

	if site.PHPVersion == "" {
		return fmt.Errorf("site %q has no PHP version set", name)
	}
	short := strings.ReplaceAll(site.PHPVersion, ".", "")
	unit := "lerd-php" + short + "-fpm"
	if err := podman.RestartUnit(unit); err != nil {
		return fmt.Errorf("restarting %s: %w", unit, err)
	}
	fmt.Printf("Restarted: %s (%s)\n", name, unit)
	return nil
}
