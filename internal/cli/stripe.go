package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
	"github.com/spf13/cobra"
)

// NewStripeCmds returns Stripe-related subcommands.
func NewStripeCmds() []*cobra.Command {
	return []*cobra.Command{
		newStripeListenCmd(),
		newStripeListenStopCmd(),
		newStripeConfigCmd(),
	}
}

func newStripeConfigCmd() *cobra.Command {
	var webhookPath string
	var secretEnvKey string

	cmd := &cobra.Command{
		Use:   "stripe:config",
		Short: "Show or set the Stripe webhook path and secret env key for the current site (without starting the listener)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			site, err := ensureSiteForCwd(cwd)
			if err != nil {
				return err
			}

			// No flags: report the current config instead of writing anything.
			if !cmd.Flags().Changed("path") && !cmd.Flags().Changed("secret-env-key") {
				key, _ := config.ResolveStripeSecret(cwd)
				if key == "" {
					key = "(none found; looked for " + strings.Join(config.StripeSecretEnvCandidates, ", ") + ")"
				}
				fmt.Printf("Stripe config for %s\n", site.Name)
				fmt.Printf("  Webhook path:   %s\n", config.StripeWebhookPath(cwd))
				fmt.Printf("  Secret env key: %s\n", key)
				return nil
			}

			// Only persist the path when --path was actually passed; its default
			// value must not overwrite a previously-saved custom route.
			pathArg := ""
			if cmd.Flags().Changed("path") {
				pathArg = webhookPath
			}
			if err := config.SetProjectStripe(cwd, pathArg, secretEnvKey); err != nil {
				return err
			}
			fmt.Printf("Updated Stripe config for %s\n", site.Name)
			fmt.Printf("  Webhook path:   %s\n", config.StripeWebhookPath(cwd))
			// Re-forward a running listener to the new route; no-op otherwise.
			RestartStripeIfActive(site)
			return nil
		},
	}
	cmd.Flags().StringVar(&webhookPath, "path", config.DefaultStripeWebhookPath, "Webhook route path on your app")
	cmd.Flags().StringVar(&secretEnvKey, "secret-env-key", "", "Which .env key holds the Stripe secret")
	return cmd
}

func newStripeListenCmd() *cobra.Command {
	var apiKey string
	var webhookPath string
	var secretEnvKey string

	cmd := &cobra.Command{
		Use:   "stripe:listen",
		Short: "Start a Stripe webhook listener for the current site as a systemd service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			// --path and --secret-env-key persist to .lerd.yaml so the UI
			// toggle and install restore reuse them. Persist before resolving
			// so a same-invocation override takes effect immediately. Only pass
			// the path when --path was actually given, otherwise its default
			// value would clobber a previously-saved custom route.
			if cmd.Flags().Changed("path") || cmd.Flags().Changed("secret-env-key") {
				pathArg := ""
				if cmd.Flags().Changed("path") {
					pathArg = webhookPath
				}
				if err := config.SetProjectStripe(cwd, pathArg, secretEnvKey); err != nil {
					return err
				}
			}

			if apiKey == "" {
				apiKey = os.Getenv("STRIPE_SECRET")
			}
			if apiKey == "" {
				_, apiKey = config.ResolveStripeSecret(cwd)
			}
			if apiKey == "" {
				return fmt.Errorf("Stripe API key required: pass --api-key or set one of %s in .env",
					strings.Join(config.StripeSecretEnvCandidates, ", "))
			}

			base := siteURL(cwd)
			if base == "" {
				if _, err := ensureSiteForCwd(cwd); err != nil {
					return err
				}
				base = siteURL(cwd)
			}
			if base == "" {
				return errNotLinked()
			}

			siteName, err := queueSiteName(cwd)
			if err != nil {
				return err
			}

			if err := stripeStartExplicit(siteName, apiKey, base+config.StripeWebhookPath(cwd)); err != nil {
				return err
			}
			if site, err := config.FindSite(siteName); err == nil && !site.Paused {
				_ = config.SetProjectWorkers(site.Path, CollectRunningWorkerNames(site))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&apiKey, "api-key", "", "Stripe API key (defaults to the secret in .env)")
	cmd.Flags().StringVar(&webhookPath, "path", config.DefaultStripeWebhookPath, "Webhook route path on your app (persisted to .lerd.yaml)")
	cmd.Flags().StringVar(&secretEnvKey, "secret-env-key", "", "Which .env key holds the Stripe secret (persisted to .lerd.yaml)")
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

// writeStripeUnit writes (and enables on first write) the lerd-stripe-<site>
// service unit without starting it. Shared by the CLI path (starts right
// after) and the install restore path (defers start to the worker phase).
func writeStripeUnit(siteName, apiKey, forwardTo string) error {
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
	return nil
}

func stripeStartExplicit(siteName, apiKey, forwardTo string) error {
	if err := writeStripeUnit(siteName, apiKey, forwardTo); err != nil {
		return err
	}
	unitName := "lerd-stripe-" + siteName
	// podman.StartUnit (not services.Mgr.Start) so AfterUnitChange fires
	// and the dashboard reflects the new state without a manual refresh.
	if err := podman.StartUnit(unitName); err != nil {
		return fmt.Errorf("starting stripe listener: %w", err)
	}

	fmt.Printf("Stripe listener started for %s\n", siteName)
	fmt.Printf("  Forwarding to: %s\n", forwardTo)
	fmt.Printf("  Logs: %s\n", unitLogHint(unitName))
	return nil
}

// stripeKeyForSite resolves a site's Stripe secret under its configured or any
// recognised .env key, returning a descriptive error when none is set.
func stripeKeyForSite(sitePath string) (string, error) {
	if _, apiKey := config.ResolveStripeSecret(sitePath); apiKey != "" {
		return apiKey, nil
	}
	return "", fmt.Errorf("no Stripe secret found in %s/.env (looked for %s)",
		sitePath, strings.Join(config.StripeSecretEnvCandidates, ", "))
}

// StripeStartForSite starts a Stripe listener for the given site, reading the
// key and webhook path from the project's .env and .lerd.yaml.
func StripeStartForSite(siteName, sitePath, siteBaseURL string) error {
	apiKey, err := stripeKeyForSite(sitePath)
	if err != nil {
		return err
	}
	if err := stripeStartExplicit(siteName, apiKey, siteBaseURL+config.StripeWebhookPath(sitePath)); err != nil {
		return err
	}
	_ = config.AddProjectWorker(sitePath, "stripe")
	ClearIdleSuspendOnStart(siteName, sitePath, "stripe")
	return nil
}

// StripeRestoreUnit writes the stripe listener unit without starting it.
// Used by install's restore path so workers launch in phase order — FPM and
// nginx come up first, then `startRestoredServices` starts every worker.
func StripeRestoreUnit(siteName, sitePath, siteBaseURL string) error {
	apiKey, err := stripeKeyForSite(sitePath)
	if err != nil {
		return err
	}
	return writeStripeUnit(siteName, apiKey, siteBaseURL+config.StripeWebhookPath(sitePath))
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

// StripeSecretSet returns true if a Stripe secret is present in the site's .env
// under its configured or any recognised env key.
func StripeSecretSet(sitePath string) bool {
	return config.StripeSecretSet(sitePath)
}

// stripeSiteName extracts the site name from a lerd-stripe-* unit name.
func stripeSiteName(unit string) string {
	return strings.TrimPrefix(unit, "lerd-stripe-")
}
