package cli

import (
	"fmt"
	"os"

	"github.com/geodro/lerd/internal/tui"
	"github.com/geodro/lerd/internal/version"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// NewTuiCmd returns the `lerd tui` command. Opens a btop-style dashboard in
// the terminal with live site / service / worker status and keybindings for
// common start / stop / restart actions.
func NewTuiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open a terminal dashboard for sites, services, and workers",
		RunE: func(_ *cobra.Command, _ []string) error {
			if !term.IsTerminal(int(os.Stdout.Fd())) {
				return fmt.Errorf("lerd tui requires an interactive terminal")
			}
			return tui.Run(version.String())
		},
	}
}
