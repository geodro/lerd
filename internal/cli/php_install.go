package cli

import (
	"fmt"
	"regexp"

	"github.com/spf13/cobra"
)

// validPhpInstallRe accepts the same shape lerd accepts elsewhere
// (MAJOR.MINOR). Patches and pre-releases aren't first-class because
// they aren't tagged consistently on the php:X.Y-fpm-alpine base.
var validPhpInstallRe = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)

// NewPhpInstallCmd returns the `lerd php:install <version>` command.
// Builds the FPM image (if missing), writes the systemd quadlet, and starts
// the unit so the version shows up in `lerd php:list` and the dashboard.
// Idempotent — re-running just re-syncs the quadlet and re-starts the unit.
func NewPhpInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "php:install <version>",
		Short: "Install a PHP version (build FPM image + register systemd unit)",
		Long: `Provisions a PHP version end-to-end so it appears in 'lerd php:list' and the
dashboard's System -> PHP picker. Equivalent to what 'lerd init' does on a
first-link of a project pinned to that PHP, but without needing a project.

Useful for adding the legacy tier (7.4) or skipping versions you don't
otherwise need a project for (e.g. pre-loading 8.3 alongside 8.4/8.5).`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			v := args[0]
			if !validPhpInstallRe.MatchString(v) {
				return fmt.Errorf("invalid PHP version %q — expected MAJOR.MINOR (e.g. 8.3)", v)
			}
			fmt.Printf("Installing PHP %s (image + quadlet + start)...\n", v)
			if err := ensureFPMQuadlet(v); err != nil {
				return fmt.Errorf("installing PHP %s: %w", v, err)
			}
			fmt.Printf("PHP %s installed.\n", v)
			return nil
		},
	}
}
