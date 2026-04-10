package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const dashboardURL = "http://lerd.localhost"

// NewDashboardCmd returns the dashboard command.
func NewDashboardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dashboard",
		Short: "Open the Lerd dashboard in the default browser",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Printf("Opening %s\n", dashboardURL)
			return openBrowser(dashboardURL)
		},
	}
}
