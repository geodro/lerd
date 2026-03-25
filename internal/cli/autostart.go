package cli

import (
	"fmt"

	lerdSystemd "github.com/geodro/lerd/internal/systemd"
	"github.com/geodro/lerd/internal/services"
	"github.com/spf13/cobra"
)

// NewAutostartCmd returns the autostart command with enable/disable subcommands.
func NewAutostartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "autostart",
		Short: "Manage autostart on login",
	}
	cmd.AddCommand(newAutostartEnableCmd())
	cmd.AddCommand(newAutostartDisableCmd())
	cmd.AddCommand(newAutostartTrayCmd())
	return cmd
}

func newAutostartEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable",
		Short: "Enable lerd autostart on login",
		RunE: func(_ *cobra.Command, _ []string) error {
			content, err := lerdSystemd.GetUnit("lerd-autostart")
			if err != nil {
				return err
			}
			if err := services.Mgr.WriteServiceUnit("lerd-autostart", content); err != nil {
				return fmt.Errorf("writing autostart service: %w", err)
			}
			if err := services.Mgr.Enable("lerd-autostart"); err != nil {
				return fmt.Errorf("enabling autostart service: %w", err)
			}
			fmt.Println("Autostart enabled — lerd will start automatically on login.")
			return nil
		},
	}
}

func newAutostartDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Disable lerd autostart on login",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := services.Mgr.Disable("lerd-autostart"); err != nil {
				return fmt.Errorf("disabling autostart service: %w", err)
			}
			if err := services.Mgr.RemoveServiceUnit("lerd-autostart"); err != nil {
				return fmt.Errorf("removing autostart service file: %w", err)
			}
			fmt.Println("Autostart disabled — lerd will not start automatically on login.")
			return nil
		},
	}
}

func newAutostartTrayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tray",
		Short: "Manage autostart of the system tray applet",
	}
	cmd.AddCommand(newAutostartTrayEnableCmd())
	cmd.AddCommand(newAutostartTrayDisableCmd())
	return cmd
}

func newAutostartTrayEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable",
		Short: "Enable lerd tray autostart on login",
		RunE: func(_ *cobra.Command, _ []string) error {
			content, err := lerdSystemd.GetUnit("lerd-tray")
			if err != nil {
				return err
			}
			if err := services.Mgr.WriteServiceUnit("lerd-tray", content); err != nil {
				return fmt.Errorf("writing tray service: %w", err)
			}
			if err := services.Mgr.Enable("lerd-tray"); err != nil {
				return fmt.Errorf("enabling tray service: %w", err)
			}
			fmt.Println("Tray autostart enabled — lerd tray will start automatically on login.")
			return nil
		},
	}
}

func newAutostartTrayDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Disable lerd tray autostart on login",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := services.Mgr.Disable("lerd-tray"); err != nil {
				return fmt.Errorf("disabling tray service: %w", err)
			}
			if err := services.Mgr.RemoveServiceUnit("lerd-tray"); err != nil {
				return fmt.Errorf("removing tray service file: %w", err)
			}
			fmt.Println("Tray autostart disabled.")
			return nil
		},
	}
}
