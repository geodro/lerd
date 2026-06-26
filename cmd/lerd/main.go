package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bytes"
	"net/http"

	"github.com/geodro/lerd/internal/activityping"
	"github.com/geodro/lerd/internal/certs"
	"github.com/geodro/lerd/internal/cli"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	"github.com/geodro/lerd/internal/eventbus"
	"github.com/geodro/lerd/internal/feedback"
	gitpkg "github.com/geodro/lerd/internal/git"
	"github.com/geodro/lerd/internal/nginx"
	nodeDet "github.com/geodro/lerd/internal/node"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/siteops"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
	"github.com/geodro/lerd/internal/ui"
	"github.com/geodro/lerd/internal/version"
	"github.com/geodro/lerd/internal/watcher"
	"github.com/spf13/cobra"
)

// notifyLerdUI posts to the lerd-ui loopback notifier so any unit lifecycle
// change from a CLI process propagates to the dashboard in real time. It
// runs synchronously with a tight timeout: the CLI process is about to
// exit, so a background goroutine would be killed before the POST hits the
// socket. 500ms is more than enough for a loopback round-trip and is barely
// perceptible. If lerd-ui isn't running the POST fails fast and the CLI
// command still succeeds.
func notifyLerdUI(_ string) {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:7073/api/internal/notify", bytes.NewReader(nil))
	if err != nil {
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

func main() {
	// Cross-process bridge from CLI unit mutations to the running lerd-ui.
	// ui.Start reassigns this in its own process for a direct in-process
	// publish; in the CLI/MCP processes we HTTP-POST to the dashboard.
	podman.AfterUnitChange = notifyLerdUI

	root := &cobra.Command{
		Use:     "lerd",
		Short:   "Lerd — Podman-powered local PHP dev environment for Linux and macOS",
		Version: version.String(),
		// Errors are printed once below: a command that already surfaced its
		// failure through the feedback UI (a red ✗ line) is suppressed here so
		// cobra doesn't reprint the same message as a second "Error: …" line.
		SilenceErrors: true,
		// Cobra validates flags and args before PersistentPreRunE runs, so by the
		// time we get here the invocation is well-formed and any failure is a
		// runtime error, not misuse. Silencing usage now keeps the clean ✗ line
		// for runtime errors while still printing the usage block for a wrong
		// flag or arg count. No subcommand defines its own persistent hook, so
		// this runs for every command.
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			return nil
		},
	}

	// Register all subcommands
	root.AddCommand(cli.NewInstallCmd())
	root.AddCommand(cli.NewStartCmd())
	root.AddCommand(cli.NewStopCmd())
	root.AddCommand(cli.NewQuitCmd())
	root.AddCommand(cli.NewUpdateCmd(version.Version))
	root.AddCommand(cli.NewUninstallCmd())
	root.AddCommand(cli.NewParkCmd())
	root.AddCommand(cli.NewInitCmd())
	root.AddCommand(cli.NewLinkCmd())
	root.AddCommand(cli.NewUnlinkCmd())
	root.AddCommand(cli.NewRestartCmd())
	root.AddCommand(cli.NewRebuildCmd())
	root.AddCommand(cli.NewUnparkCmd())
	root.AddCommand(cli.NewSitesCmd())
	root.AddCommand(cli.NewSecureCmd())
	root.AddCommand(cli.NewUnsecureCmd())
	root.AddCommand(cli.NewUseCmd())
	root.AddCommand(cli.NewIsolateCmd())
	root.AddCommand(cli.NewIsolateNodeCmd())
	root.AddCommand(cli.NewDBIsolateCmd())
	root.AddCommand(cli.NewDBShareCmd())
	root.AddCommand(cli.NewWorktreeCmd())
	root.AddCommand(cli.NewRuntimeCmd())
	root.AddCommand(cli.NewNodeInstallCmd())
	root.AddCommand(cli.NewNodeUninstallCmd())
	root.AddCommand(cli.NewNodeUseCmd())
	root.AddCommand(cli.NewNodeManageCmd())
	root.AddCommand(cli.NewNodeUnmanageCmd())
	root.AddCommand(cli.NewJSRuntimeCmd())
	root.AddCommand(cli.NewPhpListCmd())
	root.AddCommand(cli.NewPhpRebuildCmd())
	root.AddCommand(cli.NewPhpCmd())
	root.AddCommand(cli.NewPhpShellCmd())
	root.AddCommand(cli.NewConsoleCmd())
	root.AddCommand(cli.NewTestCmd())
	root.AddCommand(cli.NewVendorBinCmd())
	root.AddCommand(cli.NewNginxCmd())
	root.AddCommand(cli.NewEnvCmd())
	root.AddCommand(cli.NewEnvRestoreCmd())
	root.AddCommand(cli.NewEnvOverrideCmd())
	root.AddCommand(cli.NewEnvCheckCmd())
	root.AddCommand(cli.NewNodeCmd())
	root.AddCommand(cli.NewNpmCmd())
	root.AddCommand(cli.NewNpxCmd())
	root.AddCommand(cli.NewComposerCmd())
	root.AddCommand(cli.NewAuthCmd())
	root.AddCommand(cli.NewServiceCmd())
	root.AddCommand(cli.NewStatusCmd())
	root.AddCommand(cli.NewTuiCmd())
	root.AddCommand(cli.NewWhichCmd())
	root.AddCommand(cli.NewCheckCmd())
	root.AddCommand(cli.NewRunCmd())
	root.AddCommand(cli.NewAboutCmd())
	root.AddCommand(cli.NewWhatsnewCmd())
	root.AddCommand(cli.NewManCmd())
	root.AddCommand(cli.NewDoctorCmd())
	root.AddCommand(cli.NewMachineCmd())
	root.AddCommand(cli.NewBugReportCmd())
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
	root.AddCommand(cli.NewHorizonCmd())
	root.AddCommand(cli.NewHorizonStartCmd())
	root.AddCommand(cli.NewHorizonStopCmd())
	root.AddCommand(cli.NewHorizonReloadCmd())
	root.AddCommand(cli.NewOctaneCmd())
	root.AddCommand(cli.NewOctaneReloadCmd())
	root.AddCommand(cli.NewAutostartCmd())
	root.AddCommand(cli.NewMCPCmd())
	root.AddCommand(cli.NewMCPInjectCmd())
	root.AddCommand(cli.NewMCPEnableGlobalCmd())
	root.AddCommand(cli.NewFetchCmd())
	root.AddCommand(cli.NewDbCmd())
	root.AddCommand(cli.NewDbImportCmd())
	root.AddCommand(cli.NewDbExportCmd())
	root.AddCommand(cli.NewDbCreateCmd())
	root.AddCommand(cli.NewDbShellCmd())
	root.AddCommand(cli.NewDbSnapshotCmd())
	root.AddCommand(cli.NewDbSnapshotsCmd())
	root.AddCommand(cli.NewDbRestoreCmd())
	root.AddCommand(cli.NewDbSnapshotRmCmd())
	root.AddCommand(cli.NewDbMoveCmd())
	root.AddCommand(cli.NewXdebugCmd())
	root.AddCommand(cli.NewDumpCmd())
	root.AddCommand(cli.NewIdleCmd())
	root.AddCommand(cli.NewWSLSetupCmd())
	root.AddCommand(cli.NewProfileCmd())
	root.AddCommand(cli.NewNotifyCmd())
	root.AddCommand(cli.NewPhpExtCmd())
	root.AddCommand(cli.NewPhpBunCmd())
	root.AddCommand(cli.NewPhpPkgCmd())
	root.AddCommand(cli.NewPestBrowserCmd())
	root.AddCommand(cli.NewPhpIniCmd())
	for _, cmd := range cli.NewStripeCmds() {
		root.AddCommand(cmd)
	}
	root.AddCommand(cli.NewShareCmd())
	root.AddCommand(cli.NewDomainCmd())
	root.AddCommand(cli.NewGroupCmd())
	root.AddCommand(cli.NewFrameworkCmd())
	root.AddCommand(cli.NewWorkerCmd())
	root.AddCommand(cli.NewWorkersCmd())
	root.AddCommand(cli.NewNewCmd())
	root.AddCommand(cli.NewSetupCmd())
	root.AddCommand(cli.NewMinioMigrateCmd())
	root.AddCommand(cli.NewImportCmd())
	root.AddCommand(cli.NewSailCmd())
	root.AddCommand(cli.NewPauseCmd())
	root.AddCommand(cli.NewUnpauseCmd())
	root.AddCommand(cli.NewTrayCmd())
	root.AddCommand(newDNSCheckCmd())
	root.AddCommand(cli.NewDNSForwarderCmd())
	root.AddCommand(cli.NewLANCmd())
	root.AddCommand(cli.NewLANExposeCmd())
	root.AddCommand(cli.NewLANUnexposeCmd())
	root.AddCommand(cli.NewLANStatusCmd())
	root.AddCommand(cli.NewLANShareCmd())
	root.AddCommand(cli.NewLANUnshareCmd())
	root.AddCommand(cli.NewRemoteSetupCmd())
	root.AddCommand(cli.NewRemoteControlCmd())
	root.AddCommand(cli.NewRemoteControlOnCmd())
	root.AddCommand(cli.NewRemoteControlOffCmd())
	root.AddCommand(cli.NewRemoteControlStatusCmd())
	root.AddCommand(newWatchCmd())
	root.AddCommand(newServeUICmd())

	maybeDispatchVendorBin(root)

	if err := root.Execute(); err != nil {
		// Only print errors the command didn't already show the user via a
		// feedback Fail line, so the same failure never appears twice.
		if !feedback.AlreadyShown(err) {
			feedback.Fail(err)
		}
		os.Exit(1)
	}
}

// maybeDispatchVendorBin rewrites os.Args to invoke the hidden `vendor-bin`
// subcommand when the first positional arg doesn't match any registered cobra
// command but does match an executable in the project's `vendor/bin` directory.
// Real lerd commands always win — this only kicks in for unknown names.
func maybeDispatchVendorBin(root *cobra.Command) {
	if len(os.Args) < 2 {
		return
	}
	first := os.Args[1]
	if first == "" || strings.HasPrefix(first, "-") {
		return
	}
	if isKnownCommand(root, first) {
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	if !cli.VendorBinExists(cwd, first) {
		return
	}
	rest := os.Args[2:]
	newArgs := make([]string, 0, len(rest)+3)
	newArgs = append(newArgs, os.Args[0], "vendor-bin", first)
	newArgs = append(newArgs, rest...)
	os.Args = newArgs
}

func isKnownCommand(root *cobra.Command, name string) bool {
	for _, c := range root.Commands() {
		if c.Name() == name {
			return true
		}
		for _, a := range c.Aliases {
			if a == name {
				return true
			}
		}
	}
	return false
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
		Short: "Check that .test DNS resolution is working (with layered breakdown on failure)",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.LoadGlobal()
			if err != nil {
				return err
			}

			if !cfg.DNS.Enabled {
				fmt.Printf("DNS managed externally: lerd-dns is disabled, sites use *.%s.\n", cfg.DNS.TLD)
				return nil
			}

			diag := dns.Diagnose(cfg.DNS.TLD)
			printDNSDiagnostic(os.Stdout, diag)
			if diag.FirstFailure >= 0 {
				os.Exit(1)
			}
			return nil
		},
	}
}

// printDNSDiagnostic writes the human-facing dns:check report for one
// Diagnostic to w. Extracted from newDNSCheckCmd so the renderer (top
// line, marker prefixes, hint-printing on Fail+Warn) can be exercised
// in tests without spawning the CLI process.
func printDNSDiagnostic(w io.Writer, diag dns.Diagnostic) {
	if diag.FirstFailure < 0 {
		fmt.Fprintf(w, "DNS is working: *.%s resolves to 127.0.0.1\n\n", diag.TLD)
	} else {
		fmt.Fprintf(w, "DNS is NOT working for .%s\n\n", diag.TLD)
	}
	for _, s := range diag.Steps {
		marker := "  "
		switch s.Status {
		case dns.StepOK:
			marker = "✓ "
		case dns.StepFail:
			marker = "✗ "
		case dns.StepWarn:
			marker = "! "
		case dns.StepSkip:
			marker = "  "
		}
		if s.Detail != "" {
			fmt.Fprintf(w, "%s%-34s %s\n", marker, s.Name, s.Detail)
		} else {
			fmt.Fprintf(w, "%s%s\n", marker, s.Name)
		}
		if (s.Status == dns.StepFail || s.Status == dns.StepWarn) && s.Hint != "" {
			fmt.Fprintf(w, "    hint: %s\n", s.Hint)
		}
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
						// Tell the UI and anyone else subscribed that the
						// sites list changed. Without this the browser keeps
						// showing the deleted site until a manual refresh.
						eventbus.Default.Publish(eventbus.KindSites)
					}
				}
			}()

			// Recover worktrees whose UI-driven install crashed mid-flight.
			go func() {
				for range time.Tick(60 * time.Second) {
					rescanWorktreeInstalls()
				}
			}()

			// Watch for git worktree additions/removals.
			go func() {
				err := watcher.WatchWorktrees(
					func() []string {
						return mainRepoSitePaths()
					},
					func(sitePath, worktreeName string) {
						if syncWorktree(sitePath, worktreeName, "added", false) {
							if err := nginx.Reload(); err != nil {
								fmt.Printf("[WARN] nginx reload: %v\n", err)
							}
						}
					},
					func(sitePath, worktreeName string) {
						if syncWorktree(sitePath, worktreeName, "changed", true) {
							if err := nginx.Reload(); err != nil {
								fmt.Printf("[WARN] nginx reload: %v\n", err)
							}
							eventbus.Default.Publish(eventbus.KindSites)
						}
					},
					func(sitePath, worktreeName string) {
						site, err := config.FindSiteByPath(sitePath)
						if err != nil {
							return
						}
						// Cleanup order on plain `git worktree remove`:
						// stop per-worktree worker units first so they
						// don't restart-loop against the deleted dir, then
						// vhost (URL stops resolving), then LAN share.
						// Isolated databases are intentionally NOT dropped
						// here — `lerd worktree remove` prompts the user
						// about the DB explicitly, and the daemon's
						// startup scanWorktrees pass catches any orphans
						// left by direct git users.
						if worktreeName != "" {
							if err := cli.StopAllWorkersForWorktree(site.Name, worktreeName); err != nil {
								fmt.Printf("[WARN] stopping worktree workers for %s/%s: %v\n", site.Name, worktreeName, err)
							}
						}
						if cleanupWorktreeVhosts(site) {
							if err := nginx.Reload(); err != nil {
								fmt.Printf("[WARN] nginx reload: %v\n", err)
							}
						}
						cli.DropOrphanedWorktreeLANShares(site, liveBranchesForSite(site))
					},
				)
				if err != nil {
					fmt.Printf("[WARN] worktree watcher: %v\n", err)
				}
			}()

			// Watch DNS health and re-apply resolver config if .test breaks.
			go watcher.WatchDNS(30*time.Second, cfg.DNS.TLD)

			// Watch host gateway reachability. A laptop that changes networks
			// (home wifi → coffee shop → mobile hotspot) ends up with a stale
			// LAN IP for host.containers.internal in the shared /etc/hosts,
			// and Xdebug silently times out until the next lerd start. The
			// watcher verifies the current entry every tick and reprobes
			// only when it stops responding. On a change, host-proxy vhosts
			// (which bake the gateway IP into proxy_pass on Linux) are
			// regenerated so they don't point at the old, now-dead address.
			watcher.OnGatewayIPChange = cli.RegenerateHostProxyVhostsOnGatewayChange
			go watcher.WatchHostGateway(30 * time.Second)

			// Self-heal exec-mode framework workers on macOS. Container mode
			// uses podman --restart=always; exec mode runs guard scripts
			// under launchd that can be left orphaned by an interrupted
			// migration or sleep/wake bridge churn. No-op on Linux.
			go watcher.WatchExecWorkers(60 * time.Second)

			// Idle-suspend: suspends/resumes workers by activity. The whole session,
			// including the source-file watcher passed here, only runs while the
			// feature is enabled; off, the watcher only binds the control socket.
			watcher.StartIdle(
				func() { notifyLerdUI("idle") },
				func(stop <-chan struct{}) error {
					return watcher.WatchSourceFiles(
						func() []watcher.SourceTarget {
							reg, err := config.LoadSites()
							if err != nil {
								return nil
							}
							var targets []watcher.SourceTarget
							for _, s := range reg.Sites {
								if s.Ignored || s.Paused {
									continue
								}
								fw, _ := config.GetFrameworkForDir(s.Framework, s.Path)
								if dirs := config.SourceWatchRoots(fw, s.Path); len(dirs) > 0 {
									targets = append(targets, watcher.SourceTarget{Key: s.Name, Dirs: dirs})
								}
								wts, _ := gitpkg.DetectWorktrees(s.Path, s.PrimaryDomain())
								for _, wt := range wts {
									key := s.Name + "/" + config.WorktreeUnitSlug(filepath.Base(wt.Path))
									if dirs := config.SourceWatchRoots(fw, wt.Path); len(dirs) > 0 {
										targets = append(targets, watcher.SourceTarget{Key: key, Dirs: dirs})
									}
								}
							}
							return targets
						},
						5*time.Second,
						activityping.Site,
						stop,
					)
				},
			)

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
						siteChanged := false

						// Custom container and host-proxy sites don't use PHP/Node
						// version detection — skip re-detection to avoid overwriting
						// the empty values with defaults.
						if !site.IsCustomContainer() && !site.IsHostProxy() {
							// Re-detect PHP version in case .lerd.yaml or .php-version changed.
							{
								phpMin, phpMax := "", ""
								if site.Framework != "" {
									if fw, fwOk := config.GetFrameworkForDir(site.Framework, sitePath); fwOk {
										phpMin, phpMax = fw.PHP.Min, fw.PHP.Max
									}
								}
								detected := phpDet.DetectVersionClamped(sitePath, phpMin, phpMax, site.PHPVersion)
								if detected != site.PHPVersion {
									fmt.Printf("PHP version changed for %s: %s -> %s\n", site.Name, site.PHPVersion, detected)
									site.PHPVersion = detected
									siteChanged = true
									if !site.Paused {
										if site.Secured {
											_ = nginx.GenerateSSLVhost(*site, detected)
										} else {
											_ = nginx.GenerateVhost(*site, detected)
										}
										if err := nginx.Reload(); err != nil {
											fmt.Printf("[WARN] nginx reload after php version change for %s: %v\n", site.Name, err)
										}
									}
								}
							}

							// Re-detect Node version in case .lerd.yaml, .node-version, or .nvmrc changed.
							if detected, detErr := nodeDet.DetectVersion(sitePath); detErr == nil && detected != site.NodeVersion {
								fmt.Printf("Node version changed for %s: %s -> %s\n", site.Name, site.NodeVersion, detected)
								site.NodeVersion = detected
								siteChanged = true
							}
						}

						if siteChanged {
							_ = config.AddSite(*site)
						}
						if err := cli.QueueRestartForSite(site.Name, sitePath, site.PHPVersion); err != nil {
							fmt.Printf("[WARN] queue restart for %s: %v\n", site.Name, err)
						}
						eventbus.Default.Publish(eventbus.KindSites)
					},
				)
				if err != nil {
					fmt.Printf("[WARN] site file watcher: %v\n", err)
				}
			}()

			// Initial setup and goroutines are live; tell systemd we're
			// ready so Type=notify unit starts unblock for any dependent
			// startup sequence (lerd-ui, lerd-tray, test harnesses).
			lerdSystemd.NotifyReady()

			return watcher.Watch(cfg.ParkedDirectories, func(projectPath string) {
				fmt.Printf("New project detected: %s\n", projectPath)
				registered, err := cli.RegisterProject(projectPath, cfg)
				if err != nil {
					fmt.Printf("[WARN] registering %s: %v\n", projectPath, err)
				} else if registered {
					if err := nginx.Reload(); err != nil {
						fmt.Printf("[WARN] nginx reload: %v\n", err)
					}
					eventbus.Default.Publish(eventbus.KindSites)
				}
			}, func(removedPath string) {
				site, err := config.FindSiteByPath(removedPath)
				if err != nil {
					return // not a registered site
				}
				fmt.Printf("Project deleted: %s (%s)\n", site.Name, removedPath)
				_ = siteops.UnlinkSiteCore(site, nil)
				eventbus.Default.Publish(eventbus.KindSites)
			})
		},
	}
}

// mainRepoSitePaths returns the paths of non-ignored sites whose .git is a directory.
// liveBranchesForSite returns the set of sanitized branches that currently
// have a worktree on disk for the given site. Used by the watcher's
// onRemoved hook so it can hand the live set to the LAN-cleanup helper.
func liveBranchesForSite(site *config.Site) map[string]bool {
	out := map[string]bool{}
	wts, err := gitpkg.DetectWorktrees(site.Path, site.PrimaryDomain())
	if err != nil {
		return out
	}
	for _, w := range wts {
		out[w.Branch] = true
	}
	return out
}

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
		if s.Ignored || s.Paused {
			continue
		}
		// Catch up on isolated-DB cleanup for any worktree removed while
		// the watcher was offline (event-driven cleanup needs fsnotify).
		site := s
		cli.DropOrphanedWorktreeDBs(&site)
		// ServableWorktrees excludes any worktree whose subdomain is owned by a
		// group secondary: the secondary serves that exact host already.
		worktrees, err := gitpkg.ServableWorktrees(s.Path, s.PrimaryDomain())
		if err != nil {
			continue
		}
		// Drop any *.<site>.conf vhost that doesn't correspond to a live
		// worktree any more — branch renames and detached→named transitions
		// leave the previous name behind, and that orphan still routes to
		// the same checkout, masking the new vhost.
		if removeStaleWorktreeVhosts(&site, worktrees) {
			generated = true
		}
		if len(worktrees) == 0 {
			continue
		}
		// Reissue once per site so the cert covers the wildcard SAN for every
		// worktree even after a daemon restart that picked up worktrees added
		// before the wildcard-SAN feature shipped.
		if s.Secured {
			if reissueErr := certs.ReissueCertForWorktree(s); reissueErr != nil {
				fmt.Printf("[WARN] reissue cert for %s: %v\n", s.PrimaryDomain(), reissueErr)
			}
		}
		for _, wt := range worktrees {
			// Skip the install when the UI/CLI holds the cross-process lock:
			// it is running composer/npm install with streamed output and
			// would race the watcher's vendor seed otherwise.
			if release, ok, _ := gitpkg.TryLockInstall(wt.Path); ok {
				gitpkg.EnsureWorktreeDeps(s.Path, wt.Path, wt.Domain, s.Secured, nil)
				release()
			}
			// Provision the isolated DB for a worktree that committed
			// db_isolated: true but was never run through `lerd worktree add`
			// (e.g. linked with the worktree already present). No-op otherwise.
			if created, err := cli.EnsureWorktreeIsolatedDB(&site, wt.Branch, wt.Path); err != nil {
				fmt.Printf("[WARN] isolated DB for worktree %s: %v\n", wt.Branch, err)
			} else if created {
				fmt.Printf("Worktree DB: created isolated database for %s\n", wt.Branch)
			}
			// Host-proxy sites mirror the parent dev command on a per-worktree
			// port behind the worktree domain; no PHP vhost or framework workers.
			if site.IsHostProxy() {
				if err := cli.SetupHostProxyWorktree(site, wt.Path, wt.Domain); err != nil {
					fmt.Printf("[WARN] worktree host-proxy for %s: %v\n", wt.Domain, err)
					continue
				}
				fmt.Printf("Worktree vhost: %s -> %s (host proxy)\n", wt.Branch, wt.Domain)
				generated = true
				continue
			}
			vhostErr := nginx.GenerateWorktreeVhostFor(wt.Domain, wt.Path, s.PHPVersion, s.PrimaryDomain(), s.Name, wt.Branch, s.Secured)
			if vhostErr != nil {
				fmt.Printf("[WARN] worktree vhost for %s: %v\n", wt.Domain, vhostErr)
				continue
			}
			// Inheritance is intentionally NOT run on the boot rescan: it only
			// fires on genuine creation (the "added" watcher event in
			// syncWorktree). Re-seeding here would resurrect an override the
			// user deliberately reset, on every daemon restart.
			fmt.Printf("Worktree vhost: %s -> %s\n", wt.Branch, wt.Domain)
			generated = true

			// Per-worktree host workers (e.g. vite) need to be (re)started
			// at daemon boot too, not just when fsnotify fires onAdded.
			// Without this, units stopped during downtime never come back.
			effectivePHP := config.WorktreePHPVersion(wt.Path, s.PHPVersion)
			cli.AutoStartOptedInWorktreeWorkers(&site, wt.Path, effectivePHP)
		}
	}
	return generated
}

// rescanWorktreeInstalls re-runs EnsureWorktreeDeps for any worktree whose
// composer/JS install never completed. Covers the case where the UI's
// streamed install crashed between `git worktree add` and EnsureWorktreeDeps
// (the watcher's onAdded already fired and skipped because the UI held the
// install lock at the time).
func rescanWorktreeInstalls() {
	reg, err := config.LoadSites()
	if err != nil {
		return
	}
	for _, s := range reg.Sites {
		if s.Ignored || s.Paused || !gitpkg.IsMainRepo(s.Path) {
			continue
		}
		wts, err := gitpkg.DetectWorktrees(s.Path, s.PrimaryDomain())
		if err != nil {
			continue
		}
		for _, wt := range wts {
			if !gitpkg.NeedsInstall(wt.Path) {
				continue
			}
			release, ok, _ := gitpkg.TryLockInstall(wt.Path)
			if !ok {
				continue
			}
			fmt.Printf("Rescan: re-running install for %s (%s)\n", wt.Branch, wt.Path)
			gitpkg.EnsureWorktreeDeps(s.Path, wt.Path, wt.Domain, s.Secured, nil)
			release()
		}
	}
}

func syncWorktree(sitePath, worktreeName, action string, pruneStale bool) bool {
	site, err := config.FindSiteByPath(sitePath)
	if err != nil {
		return false
	}
	if site.Paused {
		return false
	}
	worktrees, err := gitpkg.ServableWorktrees(sitePath, site.PrimaryDomain())
	if err != nil {
		return false
	}
	if pruneStale {
		removeStaleWorktreeVhosts(site, worktrees)
	}
	for _, wt := range worktrees {
		if wt.Name != worktreeName {
			continue
		}
		// Skip the install when the UI/CLI holds the cross-process lock:
		// it is running composer/npm install with streamed output and
		// would race the watcher's vendor seed otherwise.
		if release, ok, _ := gitpkg.TryLockInstall(wt.Path); ok {
			gitpkg.EnsureWorktreeDeps(sitePath, wt.Path, wt.Domain, site.Secured, nil)
			release()
		}
		// Provision the isolated DB for a worktree that committed
		// db_isolated: true but was never run through `lerd worktree add`.
		// Gated to genuine creation like the worker/nginx seeding below: on
		// "changed" (a commit/checkout) an already-provisioned worktree would
		// no-op anyway, and a misconfigured one would re-warn on every HEAD
		// write. The boot rescan catches worktrees that opt in while offline.
		if shouldProvisionWorktreeDBOnSync(action) {
			if created, err := cli.EnsureWorktreeIsolatedDB(site, wt.Branch, wt.Path); err != nil {
				fmt.Printf("[WARN] isolated DB for worktree %s: %v\n", wt.Branch, err)
			} else if created {
				fmt.Printf("Worktree DB: created isolated database for %s\n", wt.Branch)
			}
		}
		// Host-proxy worktrees run their own dev server behind the worktree
		// domain; wire that instead of a PHP vhost + framework workers.
		if site.IsHostProxy() {
			if err := cli.SetupHostProxyWorktree(*site, wt.Path, wt.Domain); err != nil {
				fmt.Printf("[WARN] worktree host-proxy for %s: %v\n", wt.Domain, err)
				return false
			}
			if site.Secured {
				if reissueErr := certs.ReissueCertForWorktree(*site); reissueErr != nil {
					fmt.Printf("[WARN] reissue cert for worktree %s: %v\n", wt.Domain, reissueErr)
				}
			}
			fmt.Printf("Worktree %s: %s -> %s (host proxy)\n", action, wt.Branch, wt.Domain)
			return true
		}
		effectivePHP := config.WorktreePHPVersion(wt.Path, site.PHPVersion)
		vhostErr := nginx.GenerateWorktreeVhostFor(wt.Domain, wt.Path, effectivePHP, site.PrimaryDomain(), site.Name, wt.Branch, site.Secured)
		if site.Secured {
			if reissueErr := certs.ReissueCertForWorktree(*site); reissueErr != nil {
				fmt.Printf("[WARN] reissue cert for worktree %s: %v\n", wt.Domain, reissueErr)
			}
		}
		if vhostErr != nil {
			fmt.Printf("[WARN] worktree vhost for %s: %v\n", wt.Domain, vhostErr)
			return false
		}
		// Seed the worktree's override from the main branch's, but only on
		// genuine creation ("added"). On "changed" (a commit/checkout in the
		// worktree) re-seeding would undo a deliberate reset of the override.
		if shouldInheritNginxOnSync(action) {
			_ = siteops.InheritCustomNginxConfig(site.PrimaryDomain(), wt.Domain)
		}
		fmt.Printf("Worktree %s: %s -> %s\n", action, wt.Branch, wt.Domain)

		if shouldAutoStartWorkersOnSync(action) {
			cli.AutoStartOptedInWorktreeWorkers(site, wt.Path, effectivePHP)
		}

		return true
	}
	return false
}

// "changed" fires on every HEAD write (commit, checkout, rebase);
// worktree path is stable so existing units need no kick. Resurrecting
// them clobbered user stops — issue #375.
func shouldAutoStartWorkersOnSync(action string) bool {
	return action == "added"
}

// shouldProvisionWorktreeDBOnSync gates isolated-DB creation to genuine
// worktree creation. On "changed" an already-isolated worktree is a registry
// no-op, and a worktree that can't isolate (no lerd-managed parent DB) would
// otherwise log a warning on every commit/checkout. The boot rescan still
// picks up worktrees that opted in while the watcher was offline.
func shouldProvisionWorktreeDBOnSync(action string) bool {
	return action == "added"
}

// shouldInheritNginxOnSync gates one-time inheritance of the main branch's
// nginx override to genuine worktree creation. On "changed" (or the boot
// rescan) the worktree override may have been deliberately reset, so re-seeding
// it would silently resurrect config the user removed.
func shouldInheritNginxOnSync(action string) bool {
	return action == "added"
}

// cleanupWorktreeVhosts removes all subdomain vhosts for the given site's
// domain, then re-generates for worktrees still on disk. Survivors keep their
// .env; deps and APP_URL are handled by syncWorktree on add/rename, not here.
func cleanupWorktreeVhosts(site *config.Site) bool {
	removed := removeWorktreeVhosts(site)
	worktrees, _ := gitpkg.DetectWorktrees(site.Path, site.PrimaryDomain())
	// Drop the custom nginx override + backups for worktrees that are truly
	// gone. removeWorktreeVhosts wipes every worktree vhost, so a survivor is
	// any worktree still detected on disk; only the rest are pruned.
	survivors := map[string]bool{}
	for _, wt := range worktrees {
		survivors[wt.Domain] = true
	}
	for _, domain := range removed {
		if survivors[domain] {
			continue
		}
		// Guard against a separately-registered site whose primary happens to
		// be a subdomain of this one (e.g. app.test + admin.app.test): its
		// vhost matches the suffix scan, but its override must not be deleted.
		if _, err := config.FindSiteByDomain(domain); err == nil {
			continue
		}
		_ = siteops.RemoveCustomNginxConfig(domain)
	}
	changed := len(removed) > 0
	// Shrink the cert SAN list to just the surviving worktrees so removed
	// branches drop their wildcard SAN.
	if site.Secured {
		if reissueErr := certs.ReissueCertForWorktree(*site); reissueErr != nil {
			fmt.Printf("[WARN] reissue cert for %s: %v\n", site.PrimaryDomain(), reissueErr)
		}
	}
	for _, wt := range worktrees {
		effectivePHP := config.WorktreePHPVersion(wt.Path, site.PHPVersion)
		var vhostErr error
		if site.Secured {
			vhostErr = nginx.GenerateWorktreeSSLVhost(wt.Domain, wt.Path, effectivePHP, site.PrimaryDomain(), site.Name, wt.Branch)
		} else {
			vhostErr = nginx.GenerateWorktreeVhost(wt.Domain, wt.Path, effectivePHP, site.Name, wt.Branch)
		}
		if vhostErr != nil {
			fmt.Printf("[WARN] worktree vhost for %s: %v\n", wt.Domain, vhostErr)
			continue
		}
		changed = true
	}
	return changed
}

func removeStaleWorktreeVhosts(site *config.Site, worktrees []gitpkg.Worktree) bool {
	current := map[string]bool{}
	for _, wt := range worktrees {
		current[wt.Domain+".conf"] = true
	}
	confD := config.NginxConfD()
	entries, err := os.ReadDir(confD)
	if err != nil {
		return false
	}
	suffix := "." + site.PrimaryDomain() + ".conf"
	changed := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), suffix) && !current[e.Name()] {
			// A conf whose domain belongs to a separately-registered site (e.g. a
			// group secondary at <label>.<primary>) is not a stale worktree vhost;
			// leave it alone.
			domain := strings.TrimSuffix(e.Name(), ".conf")
			if _, err := config.FindSiteByDomain(domain); err == nil {
				continue
			}
			_ = os.Remove(filepath.Join(confD, e.Name()))
			changed = true
		}
	}
	return changed
}

// removeWorktreeVhosts removes every worktree subdomain vhost for the site and
// returns the domains it removed (each vhost filename minus ".conf").
func removeWorktreeVhosts(site *config.Site) []string {
	confD := config.NginxConfD()
	entries, err := os.ReadDir(confD)
	if err != nil {
		return nil
	}
	suffix := "." + site.PrimaryDomain() + ".conf"
	var removed []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), suffix) {
			// A conf whose domain belongs to a separately-registered site (e.g. a
			// group secondary at <label>.<primary>) is not a worktree vhost; never
			// delete it, or the secondary stops being served.
			domain := strings.TrimSuffix(e.Name(), ".conf")
			if _, err := config.FindSiteByDomain(domain); err == nil {
				continue
			}
			_ = os.Remove(filepath.Join(confD, e.Name()))
			removed = append(removed, domain)
		}
	}
	return removed
}

// removeStale removes registered sites whose paths no longer exist on disk.
// Covers both parked-dir projects (caught by the fast fsnotify path when it
// fires) and manually site_link'd projects outside any park. Returns true if
// any sites were removed so the caller can reload nginx and publish events.
//
//nolint:unparam // cfg reserved for future per-park gating
func removeStale(_ *config.GlobalConfig) bool {
	reg, err := config.LoadSites()
	if err != nil {
		return false
	}

	removed := false
	for _, site := range reg.Sites {
		if site.Ignored {
			continue
		}
		if _, statErr := os.Stat(site.Path); os.IsNotExist(statErr) {
			fmt.Printf("Removing stale site: %s (%s)\n", site.Name, site.Path)
			s := site
			// Tear down the site's workers and any per-site container before
			// dropping the vhost and registry entry. Without this a host-proxy
			// site's always-restart dev server (and a custom-container/FrankenPHP
			// container) keeps running after the project directory is gone. The
			// nginx reload is batched by the caller.
			if siteops.StopSiteWorkers != nil {
				siteops.StopSiteWorkers(&s)
			}
			if s.IsCustomContainer() {
				_ = podman.StopUnit(podman.CustomContainerName(s.Name))
				podman.RemoveCustomContainer(s.Name)
				_ = podman.RemoveCustomContainerQuadlet(s.Name)
			}
			if s.IsFrankenPHP() {
				_ = podman.StopUnit(podman.FrankenPHPContainerName(s.Name))
				_ = podman.RemoveFrankenPHPQuadlet(s.Name)
			}
			_ = nginx.RemoveVhost(s.PrimaryDomain())
			_ = config.RemoveSite(s.Name)
			removed = true
		}
	}
	return removed
}
