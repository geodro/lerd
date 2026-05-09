package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/geodro/lerd/internal/certs"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	"github.com/geodro/lerd/internal/nginx"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
	"github.com/spf13/cobra"
)

// NewSecureCmd returns the secure command.
func NewSecureCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "secure [name]",
		Short: "Enable HTTPS for the current site using mkcert (cert SANs cover *.<branch>.<site>.test for worktrees)",
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

	fmt.Printf("Issuing certificate for %s...\n", site.PrimaryDomain())

	if err := certs.SecureSite(*site); err != nil {
		return err
	}

	site.Secured = true
	if err := config.AddSite(*site); err != nil {
		return fmt.Errorf("updating site registry: %w", err)
	}

	updateEnvAppURL(site.Path, "https", site.PrimaryDomain())
	_ = config.SetProjectSecured(site.Path, true)

	nginx.ReloadOrWarn("")
	restartStripeIfActive(site)
	fmt.Printf("Secured: https://%s\n", site.PrimaryDomain())
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

	fmt.Printf("Removing certificate for %s...\n", site.PrimaryDomain())

	if err := certs.UnsecureSite(*site); err != nil {
		return err
	}

	site.Secured = false
	if err := config.AddSite(*site); err != nil {
		return fmt.Errorf("updating site registry: %w", err)
	}

	updateEnvAppURL(site.Path, "http", site.PrimaryDomain())
	_ = config.SetProjectSecured(site.Path, false)

	nginx.ReloadOrWarn("")
	restartStripeIfActive(site)
	fmt.Printf("Unsecured: http://%s\n", site.PrimaryDomain())
	return nil
}

// restartStripeIfActive restarts the Stripe listener for the site if it is currently running,
// so that --forward-to picks up the new http/https scheme.
func restartStripeIfActive(site *config.Site) {
	unitName := "lerd-stripe-" + site.Name
	if !lerdSystemd.IsServiceActive(unitName) {
		return
	}
	scheme := "http"
	if site.Secured {
		scheme = "https"
	}
	baseURL := scheme + "://" + site.PrimaryDomain()
	if err := StripeStartForSite(site.Name, site.Path, baseURL); err != nil {
		fmt.Printf("[WARN] updating stripe listener unit: %v\n", err)
		return
	}
	if err := lerdSystemd.RestartService(unitName); err != nil {
		fmt.Printf("[WARN] restarting stripe listener: %v\n", err)
		return
	}
	fmt.Printf("  Restarted stripe listener → %s/stripe/webhook\n", baseURL)
}

// updateEnvAppURL syncs APP_URL plus VITE_REVERB_HOST/SCHEME/PORT in the
// project's .env to match the new TLS state, so a secure flip doesn't
// leave Vite-baked browser Echo wedged on wss://host:80.
func updateEnvAppURL(projectPath, scheme, domain string) {
	if err := envfile.SyncPrimaryDomain(projectPath, domain, scheme == "https"); err != nil {
		fmt.Printf("  [WARN] could not sync .env: %v\n", err)
	} else {
		fmt.Printf("  Updated APP_URL=%s://%s and VITE_REVERB_* in .env\n", scheme, domain)
	}
}
