package cli

import (
	"fmt"

	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
)

// NewPhpRebuildCmd returns the php:rebuild command.
func NewPhpRebuildCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "php:rebuild",
		Short: "Force-rebuild all installed PHP-FPM images",
		Long:  "Removes and rebuilds all lerd PHP-FPM container images. Run after a lerd update to pick up Containerfile changes.",
		RunE:  runPhpRebuild,
	}
}

func runPhpRebuild(_ *cobra.Command, _ []string) error {
	versions, err := phpPkg.ListInstalled()
	if err != nil {
		return fmt.Errorf("listing PHP versions: %w", err)
	}

	if len(versions) == 0 {
		fmt.Println("No PHP versions installed.")
		return nil
	}

	for _, v := range versions {
		fmt.Printf("==> Rebuilding PHP %s FPM image\n", v)
		if err := podman.RebuildFPMImage(v); err != nil {
			fmt.Printf("  [WARN] PHP %s: %v\n", v, err)
			continue
		}
	}

	fmt.Println("\nAll PHP-FPM images rebuilt.")
	fmt.Println("Restart FPM containers to use the new images:")
	for _, v := range versions {
		short := ""
		for _, c := range v {
			if c != '.' {
				short += string(c)
			}
		}
		fmt.Printf("  systemctl --user restart lerd-php%s-fpm\n", short)
	}

	return nil
}
