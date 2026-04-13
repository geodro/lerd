package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
	"github.com/spf13/cobra"
)

// NewStripeCmds returns Stripe-related subcommands.
func NewStripeCmds() []*cobra.Command {
	return []*cobra.Command{
		newStripeListenCmd(),
		newStripeListenStopCmd(),
	}
}

func newStripeListenCmd() *cobra.Command {
	var apiKey string
	var webhookPath string

	cmd := &cobra.Command{
		Use:   "stripe:listen",
		Short: "Start a Stripe webhook listener for the current site as a systemd service",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			if apiKey == "" {
				apiKey = os.Getenv("STRIPE_SECRET")
			}
			if apiKey == "" {
				apiKey = envfile.ReadKey(filepath.Join(cwd, ".env"), "STRIPE_SECRET")
			}
			if apiKey == "" {
				return fmt.Errorf("Stripe API key required: pass --api-key or set STRIPE_SECRET")
			}

			base := siteURL(cwd)
			if base == "" {
				return fmt.Errorf("no registered site found for this directory — run 'lerd link' first")
			}

			siteName, err := queueSiteName(cwd)
			if err != nil {
				return err
			}

			if err := stripeStartExplicit(siteName, apiKey, base+webhookPath); err != nil {
				return err
			}
			if site, err := config.FindSite(siteName); err == nil && !site.Paused {
				_ = config.SetProjectWorkers(site.Path, CollectRunningWorkerNames(site))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&apiKey, "api-key", "", "Stripe API key (defaults to $STRIPE_SECRET)")
	cmd.Flags().StringVar(&webhookPath, "path", "/stripe/webhook", "Webhook route path on your app")
	return cmd
}

func newStripeListenStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stripe:listen stop",
		Short: "Stop the Stripe webhook listener for the current site",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			siteName, err := queueSiteName(cwd)
			if err != nil {
				return err
			}
			if err := StripeStopForSite(siteName); err != nil {
				return err
			}
			if site, err := config.FindSite(siteName); err == nil && !site.Paused {
				_ = config.SetProjectWorkers(site.Path, CollectRunningWorkerNames(site))
			}
			return nil
		},
	}
}

func stripeStartExplicit(siteName, apiKey, forwardTo string) error {
	unitName := "lerd-stripe-" + siteName
	containerName := unitName

	unit := fmt.Sprintf(`[Unit]
Description=Lerd Stripe Listener (%s)
After=network.target

[Service]
Type=simple
Restart=on-failure
RestartSec=5
ExecStart=%s run --rm --replace --name %s --network host docker.io/stripe/stripe-cli:latest listen --api-key %s --forward-to %s --skip-verify

[Install]
WantedBy=default.target
`, siteName, podman.PodmanBin(), containerName, apiKey, forwardTo)

	changed, err := services.Mgr.WriteServiceUnitIfChanged(unitName, unit)
	if err != nil {
		return fmt.Errorf("writing service unit: %w", err)
	}
	if changed {
		if err := podman.DaemonReloadFn(); err != nil {
			return fmt.Errorf("daemon-reload: %w", err)
		}
		if err := services.Mgr.Enable(unitName); err != nil {
			fmt.Printf("[WARN] enable: %v\n", err)
		}
	}

	if err := services.Mgr.Start(unitName); err != nil {
		return fmt.Errorf("starting stripe listener: %w", err)
	}

	fmt.Printf("Stripe listener started for %s\n", siteName)
	fmt.Printf("  Forwarding to: %s\n", forwardTo)
	fmt.Printf("  Logs: %s\n", unitLogHint(unitName))
	return nil
}

// StripeStartForSite starts a Stripe listener for the given site, reading the key from its .env.
func StripeStartForSite(siteName, sitePath, siteBaseURL string) error {
	apiKey := envfile.ReadKey(filepath.Join(sitePath, ".env"), "STRIPE_SECRET")
	if apiKey == "" {
		return fmt.Errorf("STRIPE_SECRET not set in %s/.env", sitePath)
	}
	if err := stripeStartExplicit(siteName, apiKey, siteBaseURL+"/stripe/webhook"); err != nil {
		return err
	}
	_ = config.AddProjectWorker(sitePath, "stripe")
	return nil
}

// StripeStopForSite stops and removes the Stripe listener for the named site.
func StripeStopForSite(siteName string) error {
	unitName := "lerd-stripe-" + siteName

	_ = services.Mgr.Disable(unitName)
	podman.StopUnit(unitName) //nolint:errcheck

	if err := services.Mgr.RemoveServiceUnit(unitName); err != nil {
		return fmt.Errorf("removing unit file: %w", err)
	}

	if err := podman.DaemonReloadFn(); err != nil {
		fmt.Printf("[WARN] daemon-reload: %v\n", err)
	}

	fmt.Printf("Stripe listener stopped for %s\n", siteName)
	return nil
}

// StripeSecretSet returns true if STRIPE_SECRET is present in the site's .env.
func StripeSecretSet(sitePath string) bool {
	return envfile.ReadKey(filepath.Join(sitePath, ".env"), "STRIPE_SECRET") != ""
}

// stripeSiteName extracts the site name from a lerd-stripe-* unit name.
func stripeSiteName(unit string) string {
	return strings.TrimPrefix(unit, "lerd-stripe-")
}
