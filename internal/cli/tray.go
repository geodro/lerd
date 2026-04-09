package cli

import (
	"github.com/geodro/lerd/internal/tray"
	"github.com/spf13/cobra"
)

// NewTrayCmd returns the tray command.
func NewTrayCmd() *cobra.Command {
	var mono bool
	cmd := &cobra.Command{
		Use:   "tray",
		Short: "Launch the system tray applet",
		RunE: func(_ *cobra.Command, _ []string) error {
			return tray.Run(mono)
		},
	}
	cmd.Flags().BoolVar(&mono, "mono", false, "Use a monochrome template icon (OS recolors it); default is the colour icon that flips white/red with lerd state")
	return cmd
}
