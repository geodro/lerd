package cli

import (
	"fmt"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
)

// NewRebuildCmd returns the rebuild command.
func NewRebuildCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rebuild [site]",
		Short: "Rebuild the custom container image and restart the container",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name, err := resolveSiteName(args)
			if err != nil {
				return err
			}
			return RebuildSite(name)
		},
	}
}

// RebuildSite rebuilds the custom container image from the Containerfile and
// restarts the container. For PHP sites this is a no-op (use php:rebuild).
func RebuildSite(name string) error {
	site, err := config.FindSite(name)
	if err != nil {
		return fmt.Errorf("site %q not found", name)
	}
	if !site.IsCustomContainer() {
		return fmt.Errorf("site %q is not a custom container site — use 'lerd php:rebuild' for PHP sites", name)
	}

	proj, err := config.LoadProjectConfig(site.Path)
	if err != nil {
		return fmt.Errorf("loading .lerd.yaml: %w", err)
	}

	// Remove old image so build starts fresh.
	_ = podman.RemoveCustomImage(name)

	fmt.Printf("Building %s...\n", podman.CustomImageName(name))
	if err := podman.BuildCustomImage(name, site.Path, proj.Container); err != nil {
		return err
	}
	podman.StoreContainerfileHash(name, site.Path, proj.Container)

	// Restart the container to pick up the new image.
	unit := podman.CustomContainerName(name)
	running, _ := podman.ContainerRunning(unit)
	if running {
		if err := podman.RestartUnit(unit); err != nil {
			return fmt.Errorf("restarting container: %w", err)
		}
	}

	fmt.Printf("Rebuilt and restarted: %s\n", name)
	return nil
}
