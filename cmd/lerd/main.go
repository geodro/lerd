package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/cli"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	gitpkg "github.com/geodro/lerd/internal/git"
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
	root.AddCommand(cli.NewQuitCmd())
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
	root.AddCommand(cli.NewAboutCmd())
	root.AddCommand(cli.NewManCmd())
	root.AddCommand(cli.NewDoctorCmd())
	root.AddCommand(cli.NewLogsCmd())
	root.AddCommand(cli.NewOpenCmd())
	root.AddCommand(cli.NewDashboardCmd())
	root.AddCommand(cli.NewQueueCmd())
	root.AddCommand(cli.NewQueueStartCmd())
	root.AddCommand(cli.NewQueueStopCmd())
	root.AddCommand(cli.NewScheduleCmd())
	root.AddCommand(cli.NewScheduleStartCmd())
	root.AddCommand(cli.NewScheduleStopCmd())
	root.AddCommand(cli.NewReverbCmd())
	root.AddCommand(cli.NewReverbStartCmd())
	root.AddCommand(cli.NewReverbStopCmd())
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
	root.AddCommand(cli.NewFrameworkCmd())
	root.AddCommand(cli.NewWorkerCmd())
	root.AddCommand(cli.NewSetupCmd())
	root.AddCommand(cli.NewPauseCmd())
	root.AddCommand(cli.NewUnpauseCmd())
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
			if os.Getenv("LERD_DEBUG") != "" {
				watcher.SetLogger(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
					Level: slog.LevelDebug,
				})))
			}

			cfg, err := config.LoadGlobal()
			if err != nil {
				return err
			}

			fmt.Println("Lerd watcher started, monitoring:", cfg.ParkedDirectories)

			// Ensure the catch-all default vhost is always present.
			if err := nginx.EnsureDefaultVhost(); err != nil {
				fmt.Printf("[WARN] default vhost: %v\n", err)
			}

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

			// Startup scan: generate vhosts for any existing worktrees.
			if scanWorktrees() {
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

			// Watch for git worktree additions/removals.
			go func() {
				err := watcher.WatchWorktrees(
					func() []string {
						return mainRepoSitePaths()
					},
					func(sitePath, worktreeName string) {
						site, err := config.FindSiteByPath(sitePath)
						if err != nil {
							return
						}
						worktrees, err := gitpkg.DetectWorktrees(sitePath, site.Domain)
						if err != nil {
							return
						}
						phpVersion := site.PHPVersion
						for _, wt := range worktrees {
							if wt.Name == worktreeName {
								gitpkg.EnsureWorktreeDeps(sitePath, wt.Path, wt.Domain, site.Secured)
								var vhostErr error
								if site.Secured {
									vhostErr = nginx.GenerateWorktreeSSLVhost(wt.Domain, wt.Path, phpVersion, site.Domain)
								} else {
									vhostErr = nginx.GenerateWorktreeVhost(wt.Domain, wt.Path, phpVersion)
								}
								if vhostErr != nil {
									fmt.Printf("[WARN] worktree vhost for %s: %v\n", wt.Domain, vhostErr)
									return
								}
								fmt.Printf("Worktree added: %s -> %s\n", wt.Branch, wt.Domain)
								if err := nginx.Reload(); err != nil {
									fmt.Printf("[WARN] nginx reload: %v\n", err)
								}
								return
							}
						}
					},
					func(sitePath, _ string) {
						site, err := config.FindSiteByPath(sitePath)
						if err != nil {
							return
						}
						if cleanupWorktreeVhosts(site) {
							if err := nginx.Reload(); err != nil {
								fmt.Printf("[WARN] nginx reload: %v\n", err)
							}
						}
					},
				)
				if err != nil {
					fmt.Printf("[WARN] worktree watcher: %v\n", err)
				}
			}()

			// Watch DNS health and re-apply resolver config if .test breaks.
			go watcher.WatchDNS(30*time.Second, cfg.DNS.TLD)

			// Watch key site config files and signal queue:restart on change.
			go func() {
				err := watcher.WatchSiteFiles(
					func() []string {
						reg, err := config.LoadSites()
						if err != nil {
							return nil
						}
						paths := make([]string, 0, len(reg.Sites))
						for _, s := range reg.Sites {
							if !s.Ignored {
								paths = append(paths, s.Path)
							}
						}
						return paths
					},
					2*time.Second,
					func(sitePath string) {
						site, err := config.FindSiteByPath(sitePath)
						if err != nil {
							return
						}
						if err := cli.QueueRestartForSite(site.Name, sitePath, site.PHPVersion); err != nil {
							fmt.Printf("[WARN] queue restart for %s: %v\n", site.Name, err)
						}
					},
				)
				if err != nil {
					fmt.Printf("[WARN] site file watcher: %v\n", err)
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

// mainRepoSitePaths returns the paths of non-ignored sites whose .git is a directory.
func mainRepoSitePaths() []string {
	reg, err := config.LoadSites()
	if err != nil {
		return nil
	}
	var paths []string
	for _, s := range reg.Sites {
		if s.Ignored {
			continue
		}
		if gitpkg.IsMainRepo(s.Path) {
			paths = append(paths, s.Path)
		}
	}
	return paths
}

// scanWorktrees generates vhosts for all existing worktrees across all main-repo sites.
// Returns true if any vhosts were generated.
func scanWorktrees() bool {
	reg, err := config.LoadSites()
	if err != nil {
		return false
	}
	generated := false
	for _, s := range reg.Sites {
		if s.Ignored {
			continue
		}
		worktrees, err := gitpkg.DetectWorktrees(s.Path, s.Domain)
		if err != nil || len(worktrees) == 0 {
			continue
		}
		for _, wt := range worktrees {
			gitpkg.EnsureWorktreeDeps(s.Path, wt.Path, wt.Domain, s.Secured)
			var vhostErr error
			if s.Secured {
				vhostErr = nginx.GenerateWorktreeSSLVhost(wt.Domain, wt.Path, s.PHPVersion, s.Domain)
			} else {
				vhostErr = nginx.GenerateWorktreeVhost(wt.Domain, wt.Path, s.PHPVersion)
			}
			if vhostErr != nil {
				fmt.Printf("[WARN] worktree vhost for %s: %v\n", wt.Domain, vhostErr)
				continue
			}
			fmt.Printf("Worktree vhost: %s -> %s\n", wt.Branch, wt.Domain)
			generated = true
		}
	}
	return generated
}

// cleanupWorktreeVhosts removes all subdomain vhosts for the given site's domain,
// then re-generates for worktrees still on disk. Returns true if any change was made.
func cleanupWorktreeVhosts(site *config.Site) bool {
	confD := config.NginxConfD()
	entries, err := os.ReadDir(confD)
	if err != nil {
		return false
	}
	suffix := "." + site.Domain + ".conf"
	changed := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), suffix) {
			_ = os.Remove(filepath.Join(confD, e.Name()))
			changed = true
		}
	}
	// Re-generate for worktrees still present
	worktrees, _ := gitpkg.DetectWorktrees(site.Path, site.Domain)
	for _, wt := range worktrees {
		var vhostErr error
		if site.Secured {
			vhostErr = nginx.GenerateWorktreeSSLVhost(wt.Domain, wt.Path, site.PHPVersion, site.Domain)
		} else {
			vhostErr = nginx.GenerateWorktreeVhost(wt.Domain, wt.Path, site.PHPVersion)
		}
		if vhostErr != nil {
			fmt.Printf("[WARN] worktree vhost for %s: %v\n", wt.Domain, vhostErr)
		}
	}
	return changed
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
