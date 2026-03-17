package main

import (
	"fmt"
	"os"

	"github.com/geodro/lerd/internal/cli"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	"github.com/geodro/lerd/internal/version"
	"github.com/geodro/lerd/internal/watcher"
	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:     "lerd",
		Short:   "Laravel Herd for Linux — Podman-powered local dev environment",
		Version: version.String(),
	}

	// Register all subcommands
	root.AddCommand(cli.NewInstallCmd())
	root.AddCommand(cli.NewStartCmd())
	root.AddCommand(cli.NewStopCmd())
	root.AddCommand(cli.NewUpdateCmd(version.Version))
	root.AddCommand(cli.NewUninstallCmd())
	root.AddCommand(cli.NewParkCmd())
	root.AddCommand(cli.NewLinkCmd())
	root.AddCommand(cli.NewUnlinkCmd())
	root.AddCommand(cli.NewUnparkCmd())
	root.AddCommand(cli.NewSitesCmd())
	root.AddCommand(cli.NewSecureCmd())
	root.AddCommand(cli.NewUseCmd())
	root.AddCommand(cli.NewIsolateCmd())
	root.AddCommand(cli.NewIsolateNodeCmd())
	root.AddCommand(cli.NewPhpListCmd())
	root.AddCommand(cli.NewServiceCmd())
	root.AddCommand(cli.NewStatusCmd())
	root.AddCommand(newDNSCheckCmd())
	root.AddCommand(newWatchCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// newDNSCheckCmd returns the dns:check command.
func newDNSCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dns:check",
		Short: "Check that .test DNS resolution is working",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.LoadGlobal()
			if err != nil {
				return err
			}

			ok, err := dns.Check(cfg.DNS.TLD)
			if err != nil {
				return err
			}
			if ok {
				fmt.Printf("DNS is working: *.%s resolves to 127.0.0.1\n", cfg.DNS.TLD)
			} else {
				fmt.Printf("DNS is NOT working for .%s\n", cfg.DNS.TLD)
				fmt.Println("Run 'lerd install' or check NetworkManager dnsmasq configuration.")
				os.Exit(1)
			}
			return nil
		},
	}
}

// newWatchCmd returns the watch command (used by the watcher systemd service).
func newWatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "watch",
		Short:  "Watch parked directories for new projects (daemon)",
		Hidden: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.LoadGlobal()
			if err != nil {
				return err
			}

			fmt.Println("Lerd watcher started, monitoring:", cfg.ParkedDirectories)

			return watcher.Watch(cfg.ParkedDirectories, func(projectPath string) {
				fmt.Printf("New project detected: %s\n", projectPath)
			})
		},
	}
}
