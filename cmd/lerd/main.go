package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/geodro/lerd/internal/cli"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	"github.com/geodro/lerd/internal/nginx"
	"github.com/geodro/lerd/internal/ui"
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
	root.AddCommand(cli.NewUnsecureCmd())
	root.AddCommand(cli.NewUseCmd())
	root.AddCommand(cli.NewIsolateCmd())
	root.AddCommand(cli.NewIsolateNodeCmd())
	root.AddCommand(cli.NewNodeInstallCmd())
	root.AddCommand(cli.NewNodeUninstallCmd())
	root.AddCommand(cli.NewNodeUseCmd())
	root.AddCommand(cli.NewPhpListCmd())
	root.AddCommand(cli.NewPhpRebuildCmd())
	root.AddCommand(cli.NewPhpCmd())
	root.AddCommand(cli.NewArtisanCmd())
	root.AddCommand(cli.NewEnvCmd())
	root.AddCommand(cli.NewNodeCmd())
	root.AddCommand(cli.NewNpmCmd())
	root.AddCommand(cli.NewNpxCmd())
	root.AddCommand(cli.NewServiceCmd())
	root.AddCommand(cli.NewStatusCmd())
	root.AddCommand(cli.NewLogsCmd())
	root.AddCommand(cli.NewOpenCmd())
	root.AddCommand(cli.NewQueueCmd())
	root.AddCommand(cli.NewQueueStartCmd())
	root.AddCommand(cli.NewQueueStopCmd())
	root.AddCommand(cli.NewAutostartCmd())
	root.AddCommand(cli.NewMCPCmd())
	root.AddCommand(cli.NewMCPInjectCmd())
	root.AddCommand(cli.NewFetchCmd())
	root.AddCommand(cli.NewDbCmd())
	root.AddCommand(cli.NewDbImportCmd())
	root.AddCommand(cli.NewDbExportCmd())
	root.AddCommand(cli.NewDbCreateCmd())
	root.AddCommand(cli.NewDbShellCmd())
	root.AddCommand(cli.NewXdebugCmd())
	root.AddCommand(cli.NewPhpExtCmd())
	root.AddCommand(cli.NewPhpIniCmd())
	for _, cmd := range cli.NewStripeCmds() {
		root.AddCommand(cmd)
	}
	root.AddCommand(cli.NewShareCmd())
	root.AddCommand(cli.NewSetupCmd())
	root.AddCommand(cli.NewTrayCmd())
	root.AddCommand(newDNSCheckCmd())
	root.AddCommand(newWatchCmd())
	root.AddCommand(newServeUICmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// newServeUICmd returns the serve-ui command.
func newServeUICmd() *cobra.Command {
	return &cobra.Command{
		Use:    "serve-ui",
		Short:  "Start the Lerd UI dashboard server",
		Hidden: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return ui.Start(version.Version)
		},
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

			// Initial scan: register new projects.
			reloadNeeded := false
			for _, dir := range cfg.ParkedDirectories {
				entries, err := os.ReadDir(dir)
				if err != nil {
					continue
				}
				for _, entry := range entries {
					if !entry.IsDir() {
						continue
					}
					registered, err := cli.RegisterProject(filepath.Join(dir, entry.Name()), cfg)
					if err != nil {
						fmt.Printf("[WARN] %s: %v\n", entry.Name(), err)
					} else if registered {
						reloadNeeded = true
					}
				}
			}

			// Remove stale sites (deleted while we were offline or during the scan above).
			if removeStale(cfg) {
				reloadNeeded = true
			}

			if reloadNeeded {
				if err := nginx.Reload(); err != nil {
					fmt.Printf("[WARN] nginx reload: %v\n", err)
				}
			}

			// Periodically catch deletions that happen while the watcher is busy.
			go func() {
				for range time.Tick(30 * time.Second) {
					if removeStale(cfg) {
						if err := nginx.Reload(); err != nil {
							fmt.Printf("[WARN] nginx reload: %v\n", err)
						}
					}
				}
			}()

			return watcher.Watch(cfg.ParkedDirectories, func(projectPath string) {
				fmt.Printf("New project detected: %s\n", projectPath)
				registered, err := cli.RegisterProject(projectPath, cfg)
				if err != nil {
					fmt.Printf("[WARN] registering %s: %v\n", projectPath, err)
				} else if registered {
					if err := nginx.Reload(); err != nil {
						fmt.Printf("[WARN] nginx reload: %v\n", err)
					}
				}
			}, func(removedPath string) {
				site, err := config.FindSiteByPath(removedPath)
				if err != nil {
					return // not a registered site
				}
				fmt.Printf("Project deleted: %s (%s)\n", site.Name, removedPath)
				_ = nginx.RemoveVhost(site.Domain)
				if err := config.RemoveSite(site.Name); err != nil {
					fmt.Printf("[WARN] removing site %s: %v\n", site.Name, err)
					return
				}
				if err := nginx.Reload(); err != nil {
					fmt.Printf("[WARN] nginx reload: %v\n", err)
				}
			})
		},
	}
}

// removeStale removes registered sites under parked directories whose paths no
// longer exist on disk. Returns true if any sites were removed.
func removeStale(cfg *config.GlobalConfig) bool {
	reg, err := config.LoadSites()
	if err != nil {
		return false
	}

	removed := false
	for _, site := range reg.Sites {
		if site.Ignored {
			continue
		}
		underParked := false
		for _, dir := range cfg.ParkedDirectories {
			if filepath.Dir(site.Path) == dir {
				underParked = true
				break
			}
		}
		if !underParked {
			continue
		}
		if _, statErr := os.Stat(site.Path); os.IsNotExist(statErr) {
			fmt.Printf("Removing stale site: %s (%s)\n", site.Name, site.Path)
			_ = nginx.RemoveVhost(site.Domain)
			_ = config.RemoveSite(site.Name)
			removed = true
		}
	}
	return removed
}
