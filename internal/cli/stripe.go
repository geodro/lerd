package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

// NewStripeCmds returns Stripe-related subcommands.
func NewStripeCmds() []*cobra.Command {
	return []*cobra.Command{
		newStripeListenCmd(),
	}
}

func newStripeListenCmd() *cobra.Command {
	var apiKey string
	var webhookPath string

	cmd := &cobra.Command{
		Use:   "stripe:listen",
		Short: "Forward Stripe webhooks to the current site via the Stripe CLI",
		Long: `Runs the Stripe CLI in a temporary container to forward live/test webhook
events from Stripe to your local app.

The forward URL is auto-detected from the current site. Override the webhook
path with --path if your app uses a custom route.

Example:
  lerd stripe:listen
  lerd stripe:listen --path /webhooks/stripe`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if apiKey == "" {
				apiKey = os.Getenv("STRIPE_API_KEY")
			}
			if apiKey == "" {
				return fmt.Errorf("Stripe API key required: pass --api-key or set STRIPE_API_KEY")
			}

			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			base := siteURL(cwd)
			if base == "" {
				return fmt.Errorf("no registered site found for this directory — run 'lerd link' first")
			}
			forwardTo := base + webhookPath

			fmt.Printf("Forwarding Stripe webhooks → %s\n", forwardTo)
			fmt.Println("Press Ctrl+C to stop.")

			cmdArgs := []string{
				"run", "--rm", "-it",
				"--network", "lerd",
				"docker.io/stripe/stripe-cli:latest",
				"listen",
				"--api-key", apiKey,
				"--forward-to", forwardTo,
				"--skip-verify",
			}

			cmd := exec.Command("podman", cmdArgs...)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		},
	}
	cmd.Flags().StringVar(&apiKey, "api-key", "", "Stripe API key (defaults to $STRIPE_API_KEY)")
	cmd.Flags().StringVar(&webhookPath, "path", "/stripe/webhook", "Webhook route path on your app")
	return cmd
}
