package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/geodro/lerd/internal/certs"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
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

// NewUnsecureCmd returns the unsecure command.
func NewUnsecureCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unsecure [name]",
		Short: "Disable HTTPS for the current site",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runUnsecure,
	}
}

func resolveSiteName(args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Base(cwd), nil
}

func runSecure(_ *cobra.Command, args []string) error {
	name, err := resolveSiteName(args)
	if err != nil {
		return err
	}

	site, err := config.FindSite(name)
	if err != nil {
		return fmt.Errorf("site %q not found — run 'lerd link' first", name)
	}

	fmt.Printf("Issuing certificate for %s...\n", site.Domain)

	if err := certs.SecureSite(*site); err != nil {
		return err
	}

	site.Secured = true
	if err := config.AddSite(*site); err != nil {
		return fmt.Errorf("updating site registry: %w", err)
	}

	updateEnvAppURL(site.Path, "https", site.Domain)

	fmt.Printf("Secured: https://%s\n", site.Domain)
	return nil
}

func runUnsecure(_ *cobra.Command, args []string) error {
	name, err := resolveSiteName(args)
	if err != nil {
		return err
	}

	site, err := config.FindSite(name)
	if err != nil {
		return fmt.Errorf("site %q not found — run 'lerd link' first", name)
	}

	fmt.Printf("Removing certificate for %s...\n", site.Domain)

	if err := certs.UnsecureSite(*site); err != nil {
		return err
	}

	site.Secured = false
	if err := config.AddSite(*site); err != nil {
		return fmt.Errorf("updating site registry: %w", err)
	}

	updateEnvAppURL(site.Path, "http", site.Domain)

	fmt.Printf("Unsecured: http://%s\n", site.Domain)
	return nil
}

// updateEnvAppURL sets APP_URL in the project's .env to scheme://domain.
// Silently does nothing if no .env exists.
func updateEnvAppURL(projectPath, scheme, domain string) {
	if err := envfile.UpdateAppURL(projectPath, scheme, domain); err != nil {
		fmt.Printf("  [WARN] could not update APP_URL in .env: %v\n", err)
	} else {
		fmt.Printf("  Updated APP_URL=%s://%s\n", scheme, domain)
	}
}
