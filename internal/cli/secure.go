package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/geodro/lerd/internal/certs"
	"github.com/geodro/lerd/internal/config"
	"github.com/spf13/cobra"
)

// NewSecureCmd returns the secure command.
func NewSecureCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "secure [name]",
		Short: "Enable HTTPS for the current site using mkcert",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runSecure,
	}
}

func runSecure(_ *cobra.Command, args []string) error {
	name := ""
	if len(args) > 0 {
		name = args[0]
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		name = filepath.Base(cwd)
	}

	site, err := config.FindSite(name)
	if err != nil {
		return fmt.Errorf("site %q not found — run 'lerd link' first", name)
	}

	fmt.Printf("Issuing certificate for %s...\n", site.Domain)

	if err := certs.SecureSite(site.Domain, site.PHPVersion); err != nil {
		return err
	}

	site.Secured = true
	if err := config.AddSite(*site); err != nil {
		return fmt.Errorf("updating site registry: %w", err)
	}

	fmt.Printf("Secured: https://%s\n", site.Domain)
	return nil
}
