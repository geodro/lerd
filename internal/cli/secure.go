package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/geodro/lerd/internal/certs"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	"github.com/geodro/lerd/internal/nginx"
	"github.com/geodro/lerd/internal/services"
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
	// Look up by path first so directory names like "astrolov.com" resolve
	// correctly to their registered site name (e.g. "astrolov").
	if site, err := config.FindSiteByPath(cwd); err == nil {
		return site.Name, nil
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
	syncProjectConfigSecured(site.Path, true)

	if err := nginx.Reload(); err != nil {
		fmt.Printf("[WARN] nginx reload: %v\n", err)
	}
	restartStripeIfActive(site)
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
	syncProjectConfigSecured(site.Path, false)

	if err := nginx.Reload(); err != nil {
		fmt.Printf("[WARN] nginx reload: %v\n", err)
	}
	restartStripeIfActive(site)
	fmt.Printf("Unsecured: http://%s\n", site.Domain)
	return nil
}

// syncProjectConfigSecured updates the secured field in .lerd.yaml when the
// file already exists, keeping the saved config in sync with secure/unsecure calls.
func syncProjectConfigSecured(projectPath string, secured bool) {
	lerdYAML := filepath.Join(projectPath, ".lerd.yaml")
	if _, err := os.Stat(lerdYAML); err != nil {
		return
	}
	proj, _ := config.LoadProjectConfig(projectPath)
	proj.Secured = secured
	if err := config.SaveProjectConfig(projectPath, proj); err != nil {
		fmt.Printf("  [WARN] updating .lerd.yaml: %v\n", err)
	}
}

// restartStripeIfActive restarts the Stripe listener for the site if it is currently running,
// so that --forward-to picks up the new http/https scheme.
func restartStripeIfActive(site *config.Site) {
	unitName := "lerd-stripe-" + site.Name
	if !services.Mgr.IsActive(unitName) {
		return
	}
	scheme := "http"
	if site.Secured {
		scheme = "https"
	}
	baseURL := scheme + "://" + site.Domain
	if err := StripeStartForSite(site.Name, site.Path, baseURL); err != nil {
		fmt.Printf("[WARN] updating stripe listener unit: %v\n", err)
		return
	}
	if err := services.Mgr.Restart(unitName); err != nil {
		fmt.Printf("[WARN] restarting stripe listener: %v\n", err)
		return
	}
	fmt.Printf("  Restarted stripe listener → %s/stripe/webhook\n", baseURL)
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
