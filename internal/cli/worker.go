package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	gitpkg "github.com/geodro/lerd/internal/git"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
	"github.com/spf13/cobra"
)

// NewWorkerCmd returns the worker parent command with start/stop/list subcommands.
func NewWorkerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Manage framework-defined workers for the current site",
	}
	cmd.AddCommand(newWorkerStartCmd())
	cmd.AddCommand(newWorkerStopCmd())
	cmd.AddCommand(newWorkerListCmd())
	cmd.AddCommand(newWorkerAddCmd())
	cmd.AddCommand(newWorkerRemoveCmd())
	cmd.AddCommand(newWorkerHealCmd())
	return cmd
}

func newWorkerStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <name>",
		Short: "Start a framework worker as a systemd service",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			workerName := args[0]
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			site, fw, phpVersion, err := resolveSiteAndFramework(cwd)
			if err != nil {
				return err
			}
			worker, ok := fw.Workers[workerName]
			if !ok {
				return fmt.Errorf("framework %q has no worker named %q\nRun 'lerd worker list' to see available workers", fw.Label, workerName)
			}
			if worker.Check != nil && !config.MatchesRule(cwd, *worker.Check) {
				return fmt.Errorf("worker %q requires a dependency that is not installed\nCheck the framework definition for required packages", workerName)
			}
			if err := WorkerStartForSite(site.Name, cwd, phpVersion, workerName, worker, true); err != nil {
				return err
			}
			if !site.Paused {
				_ = config.SetProjectWorkers(site.Path, CollectRunningWorkerNames(site))
			}
			return nil
		},
	}
}

func newWorkerStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <name>",
		Short: "Stop a framework worker",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			workerName := args[0]
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			site, fw, _, err := resolveSiteAndFramework(cwd)
			if err != nil {
				return err
			}
			// Allow stopping orphaned workers that have a running unit
			// but are no longer in the framework definition.
			if _, ok := fw.Workers[workerName]; !ok {
				unitName := "lerd-" + workerName + "-" + site.Name
				if !isServiceActiveOrRestarting(unitName) {
					return fmt.Errorf("framework %q has no worker named %q\nRun 'lerd worker list' to see available workers", fw.Label, workerName)
				}
			}
			if err := WorkerStopForSite(site.Name, cwd, workerName); err != nil {
				return err
			}
			if !site.Paused {
				_ = config.SetProjectWorkers(site.Path, CollectRunningWorkerNames(site))
			}
			return nil
		},
	}
}

func newWorkerListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List workers defined for the current site's framework",
		RunE: func(_ *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			site, fw, _, err := resolveSiteAndFramework(cwd)
			if err != nil {
				return err
			}
			known := make(map[string]bool)
			if len(fw.Workers) == 0 {
				fmt.Printf("Framework %q has no workers defined.\n", fw.Label)
			} else {
				names := make([]string, 0, len(fw.Workers))
				for n, wDef := range fw.Workers {
					if wDef.Check != nil && !config.MatchesRule(cwd, *wDef.Check) {
						continue
					}
					names = append(names, n)
				}
				sort.Strings(names)
				fmt.Printf("Workers for %s:\n", fw.Label)
				for _, name := range names {
					known[name] = true
					w := fw.Workers[name]
					label := w.Label
					if label == "" {
						label = name
					}
					fmt.Printf("  %-15s %s\n", name, label)
					fmt.Printf("  %-15s command: %s\n", "", w.Command)
				}
			}

			// Detect orphaned workers — running units with no definition.
			orphans := findOrphanedWorkers(site.Name, known)
			if len(orphans) > 0 {
				fmt.Println("\nOrphaned workers (running but not defined):")
				for _, name := range orphans {
					fmt.Printf("  %-15s (stop with: lerd worker stop %s)\n", name, name)
				}
			}
			return nil
		},
	}
}

// resolveSiteAndFramework finds the registered site and its framework for cwd.
// Falls back to framework detection if the site has no Framework set.
// For custom container sites without a framework, a synthetic framework is
// returned that contains only the custom_workers from .lerd.yaml.
func resolveSiteAndFramework(cwd string) (*config.Site, *config.Framework, string, error) {
	site, err := config.FindSiteByPath(cwd)
	if err != nil {
		if parent, ok := config.ParentSiteForWorktreeDir(cwd); ok {
			site = parent
		} else {
			site, err = ensureSiteForCwd(cwd)
			if err != nil {
				return nil, nil, "", err
			}
		}
	}

	// Custom container sites may not have a framework. Build a synthetic
	// framework from .lerd.yaml custom_workers so the worker commands work.
	if site.IsCustomContainer() && site.Framework == "" {
		fw := &config.Framework{Name: "custom", Label: "custom container"}
		if proj, _ := config.LoadProjectConfig(cwd); proj != nil && len(proj.CustomWorkers) > 0 {
			fw.Workers = proj.CustomWorkers
		}
		return site, fw, "", nil
	}

	fwName := site.Framework
	if fwName == "" {
		return nil, nil, "", fmt.Errorf("site %q has no framework assigned — run 'lerd link' first", site.Name)
	}

	fw, ok := config.GetFrameworkForDir(fwName, cwd)
	if !ok {
		return nil, nil, "", fmt.Errorf("site %q has no framework assigned — run 'lerd link' or 'lerd framework add'", site.Name)
	}

	phpVersion := site.PHPVersion
	if phpVersion == "" {
		phpVersion, err = phpDet.DetectVersion(cwd)
		if err != nil {
			cfg, _ := config.LoadGlobal()
			phpVersion = cfg.PHP.DefaultVersion
		}
	}

	return site, fw, phpVersion, nil
}

// requireFrameworkWorker returns an error if the site's framework doesn't define the named worker.
func requireFrameworkWorker(cwd, workerName string) error {
	_, fw, _, err := resolveSiteAndFramework(cwd)
	if err != nil {
		return err
	}
	if fw.Workers == nil {
		return fmt.Errorf("framework %q has no workers defined", fw.Label)
	}
	if _, ok := fw.Workers[workerName]; !ok {
		return fmt.Errorf("framework %q has no worker named %q\nRun 'lerd worker list' to see available workers", fw.Label, workerName)
	}
	return nil
}

// resolveWorkerCommand returns the command to run for a worker, substituting the
// worker's reload variant (restart on file changes) when the project opted the
// worker into reload mode and the framework declares the variant. The variant
// text comes from the framework definition (FrameworkWorker.ReloadCommand), so
// the store stays the single source of truth and core never rewrites command
// strings.
//
// The polling flag is appended where the watcher can't see host filesystem
// events. lerd runs workers inside a container; on macOS that container lives
// in the podman virtual machine while the project is shared in from the host,
// so inotify events raised on the host never reach the watcher in the VM. Under
// WSL2 the same gap exists for projects on 9p (/mnt) mounts, where inotify is
// not delivered across the boundary. On native Linux the container shares the
// host filesystem directly and inotify works, so polling is left off to avoid
// the wasted CPU. See watcherNeedsPolling.
//
// The reload command's watcher shells out to node and resolves the chokidar npm
// package from the project's node_modules; when chokidar is missing we keep the
// standard command and tell the user how to enable the watcher rather than
// letting the worker fail to boot. Enabling reload from the CLI or UI refuses
// up front when chokidar is absent (see ApplyHorizonReload), so this fallback
// only bites if the package is removed after the fact.
func resolveWorkerCommand(sitePath, workerName string, w config.FrameworkWorker) string {
	if w.ReloadCommand == "" || !config.ProjectReloadsWorker(sitePath, workerName) {
		return w.Command
	}
	if !projectHasChokidar(sitePath) {
		fmt.Printf("[WARN] %s auto-reload is on but chokidar is not installed in %s, running the standard command. Install it with: npm install -D chokidar\n", workerName, sitePath)
		return w.Command
	}
	command := w.ReloadCommand
	if watcherNeedsPolling(sitePath) {
		command += " --poll"
	}
	return command
}

// watcherNeedsPolling reports whether the reload watcher has to poll because
// host filesystem events don't reach it. Delegates to config.WatcherNeedsPolling
// (the canonical predicate, shared with the Octane reload path).
func watcherNeedsPolling(sitePath string) bool {
	return config.WatcherNeedsPolling(sitePath)
}

// projectHasChokidar reports whether the chokidar package, required by the
// reload command's file watcher, is installed in the project. Delegates to
// config.ProjectHasChokidar.
func projectHasChokidar(sitePath string) bool {
	return config.ProjectHasChokidar(sitePath)
}

// ProjectHasChokidar is the exported view of projectHasChokidar, for the UI
// snapshot to report whether Horizon auto-reload can be enabled for a site.
func ProjectHasChokidar(sitePath string) bool {
	return projectHasChokidar(sitePath)
}

// InstallChokidar runs `npm install --save-dev chokidar` in the project (via
// runNpmCaptured, the shared fnm helper) so Horizon's horizon:listen watcher
// can resolve chokidar (Vite 8 no longer ships it transitively). Output is
// folded into the error on failure so the UI can show why it failed.
func InstallChokidar(sitePath string) error {
	out, err := runNpmCaptured(sitePath, "install", "--save-dev", "chokidar")
	if err != nil {
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("npm install --save-dev chokidar failed: %s", msg)
	}
	return nil
}

// WorkerStartForSite writes a systemd unit for the given framework worker and starts it.
// The unit name is lerd-{workerName}-{siteName}.
// If the worker has a Proxy config, the proxy port is auto-assigned and the
// nginx vhost is regenerated to include the WebSocket/HTTP proxy block.
// When persist is false the worker is not added to .lerd.yaml, used by the
// auto-start path so worktree vite workers don't appear as user-opted entries.
func WorkerStartForSite(siteName, sitePath, phpVersion, workerName string, w config.FrameworkWorker, persist bool) error {
	if err := workerStartPreflight(sitePath, workerName, w); err != nil {
		return err
	}

	// Skip lifecycle for worker shapes the current platform can't run.
	// Without this gate macOS hosts proceed past writeWorkerUnitFile (which
	// returns (false, nil) and prints a WARN) into podman.StartUnit on a
	// unit that was never written, surfacing a confusing podman error
	// behind the original WARN.
	if ok, reason := workerSupportedOnPlatform(w); !ok {
		fmt.Printf("[WARN] worker %s skipped: %s\n", workerName, reason)
		return nil
	}

	// Stop conflicting workers before starting. Match the new worker's
	// path so a per-worktree start tears down only the same worktree's
	// conflicting unit and doesn't touch the parent's.
	for _, conflict := range w.ConflictsWith {
		WorkerStopForSite(siteName, sitePath, conflict) //nolint:errcheck
	}

	command := resolveWorkerCommand(sitePath, workerName, w)

	// Handle proxy port assignment and command augmentation.
	if w.Proxy != nil && w.Proxy.PortEnvKey != "" {
		envPath := filepath.Join(sitePath, ".env")
		port := envfile.ReadKey(envPath, w.Proxy.PortEnvKey)
		if port == "" {
			port = strconv.Itoa(assignWorkerProxyPort(sitePath, w.Proxy.PortEnvKey, w.Proxy.DefaultPort))
			_ = envfile.ApplyUpdates(envPath, map[string]string{w.Proxy.PortEnvKey: port})
		}
		command = command + " --port=" + port
	}

	// Workers exec into the container that hosts the site's runtime —
	// custom container, FrankenPHP, or shared FPM. resolveWorkerFPMUnit
	// owns the per-runtime mapping; restoreWorker / writeWorkerExecUnit
	// share the same helper.
	fpmUnit := resolveWorkerFPMUnit(siteName, phpVersion)
	unitName, unitSiteName := workerNames(siteName, sitePath, workerName)

	restart := w.Restart
	if restart == "" {
		restart = "always"
	}
	label := w.Label
	if label == "" {
		label = workerName
	}

	changed, err := writeWorkerUnitFile(unitName, label, unitSiteName, sitePath, phpVersion, command, restart, w.Schedule, fpmUnit, w.Host)
	if err != nil {
		return fmt.Errorf("writing worker unit: %w", err)
	}

	// Scheduled workers run via a sibling .timer that systemd starts on
	// the configured cadence; the .service is a Type=oneshot triggered by
	// the timer, so we enable and start the .timer rather than the
	// .service. The non-scheduled (daemon) path keeps the original
	// .service-based lifecycle.
	lifecycleTarget := unitName
	if w.Schedule != "" {
		lifecycleTarget = unitName + ".timer"
	}

	if changed {
		// A rewritten unit (e.g. a runtime switch re-pointed the worker at a
		// different container) only takes effect once systemd re-reads it;
		// without this, Enable/Start act on the stale cached unit.
		if err := podman.DaemonReloadFn(); err != nil {
			fmt.Printf("[WARN] daemon-reload: %v\n", err)
		}
		if err := services.Mgr.Enable(lifecycleTarget); err != nil {
			fmt.Printf("[WARN] enable: %v\n", err)
		}
	}

	// Route through podman.StartUnit (not services.Mgr.Start directly) so
	// AfterUnitChange fires the dashboard cache invalidate + WS push. On
	// Linux the systemd DBus subscription catches direct services.Mgr
	// calls as a fallback; macOS has no equivalent, so a direct call
	// leaves the UI stale until the next 15s cache poll.
	if err := podman.StartUnit(lifecycleTarget); err != nil {
		return fmt.Errorf("starting %s worker: %w", workerName, err)
	}

	fmt.Printf("%s started for %s\n", label, siteName)
	fmt.Printf("  Logs: %s\n", workerLogHint(unitName, w.Host))

	// A running worker can't be idle-suspended: clear any stale entry so the idle
	// engine doesn't boot believing this site (or worktree) is still asleep.
	ClearIdleSuspendOnStart(siteName, sitePath, workerName)

	// Regenerate nginx vhost if the worker has proxy config.
	if w.Proxy != nil {
		regenNginxVhost(siteName, sitePath)
	}

	// Persist this worker to .lerd.yaml so lerd install can restore it.
	// Additive: other workers already in the list are not removed. Skipped
	// when persist is false (auto-start path) so worktree workers don't
	// appear as user-opted entries.
	if persist {
		_ = config.AddProjectWorker(sitePath, workerName)
	}

	return nil
}

func newWorkerAddCmd() *cobra.Command {
	var (
		command       string
		label         string
		restart       string
		checkFile     string
		checkComposer string
		conflictsWith []string
		proxyPath     string
		proxyPortKey  string
		proxyDefPort  int
		global        bool
	)

	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a custom worker to this project or global framework overlay",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			site, err := ensureSiteForCwd(cwd)
			if err != nil {
				return err
			}

			w := config.FrameworkWorker{
				Label:         label,
				Command:       command,
				Restart:       restart,
				ConflictsWith: conflictsWith,
			}
			if checkFile != "" || checkComposer != "" {
				w.Check = &config.FrameworkRule{File: checkFile, Composer: checkComposer}
			}
			if proxyPath != "" {
				w.Proxy = &config.WorkerProxy{
					Path:        proxyPath,
					PortEnvKey:  proxyPortKey,
					DefaultPort: proxyDefPort,
				}
			}

			action := "added"
			if global {
				fwName := site.Framework
				if fwName == "" {
					return fmt.Errorf("site %q has no framework assigned", site.Name)
				}
				fw := config.LoadUserFramework(fwName)
				if fw == nil {
					fw = &config.Framework{Name: fwName}
				}
				if fw.Workers == nil {
					fw.Workers = make(map[string]config.FrameworkWorker)
				}
				if _, exists := fw.Workers[name]; exists {
					action = "updated"
				}
				fw.Workers[name] = w
				if err := config.SaveFramework(fw); err != nil {
					return fmt.Errorf("saving framework overlay: %w", err)
				}
				fmt.Printf("Custom worker %q %s in global %s overlay\n", name, action, fwName)
			} else {
				if proj, _ := config.LoadProjectConfig(cwd); proj.CustomWorkers != nil {
					if _, exists := proj.CustomWorkers[name]; exists {
						action = "updated"
					}
				}
				if err := config.SetProjectCustomWorker(cwd, name, w); err != nil {
					return fmt.Errorf("saving .lerd.yaml: %w", err)
				}
				fmt.Printf("Custom worker %q %s in .lerd.yaml\n", name, action)
			}
			fmt.Printf("Start it with: lerd worker start %s\n", name)
			return nil
		},
	}

	cmd.Flags().StringVar(&command, "command", "", "Command to run (required)")
	cmd.Flags().StringVar(&label, "label", "", "Human-readable label")
	cmd.Flags().StringVar(&restart, "restart", "", "Restart policy: always or on-failure")
	cmd.Flags().StringVar(&checkFile, "check-file", "", "Only show worker when this file exists")
	cmd.Flags().StringVar(&checkComposer, "check-composer", "", "Only show worker when this Composer package is installed")
	cmd.Flags().StringSliceVar(&conflictsWith, "conflicts-with", nil, "Workers to stop before starting this one")
	cmd.Flags().StringVar(&proxyPath, "proxy-path", "", "URL path to proxy (e.g. /app)")
	cmd.Flags().StringVar(&proxyPortKey, "proxy-port-env-key", "", "Env key holding the worker port")
	cmd.Flags().IntVar(&proxyDefPort, "proxy-default-port", 0, "Default port if env key is missing")
	cmd.Flags().BoolVar(&global, "global", false, "Save to global framework overlay instead of .lerd.yaml")
	_ = cmd.MarkFlagRequired("command")

	return cmd
}

func newWorkerRemoveCmd() *cobra.Command {
	var global bool

	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a custom worker from .lerd.yaml or global framework overlay",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			site, err := ensureSiteForCwd(cwd)
			if err != nil {
				return err
			}

			// Stop the worker if running — on the parent and on every
			// worktree. Without the worktree pass, per-worktree units
			// (lerd-<name>-<site>-<wt>) keep running against a
			// definition that's about to be deleted from .lerd.yaml.
			paths := []string{site.Path}
			if worktrees, err := gitpkg.DetectWorktrees(site.Path, site.PrimaryDomain()); err == nil {
				for _, wt := range worktrees {
					paths = append(paths, wt.Path)
				}
			}
			for _, p := range paths {
				unit := workerUnitName(site.Name, p, name)
				if isServiceActiveOrRestarting(unit) {
					_ = WorkerStopForSite(site.Name, p, name)
				}
			}

			if global {
				fwName := site.Framework
				if fwName == "" {
					return fmt.Errorf("site %q has no framework assigned", site.Name)
				}
				fw := config.LoadUserFramework(fwName)
				if fw == nil || fw.Workers == nil {
					return fmt.Errorf("no global overlay for framework %q", fwName)
				}
				if _, exists := fw.Workers[name]; !exists {
					return fmt.Errorf("worker %q not found in global %s overlay", name, fwName)
				}
				delete(fw.Workers, name)
				if len(fw.Workers) == 0 {
					fw.Workers = nil
				}
				if err := config.SaveFramework(fw); err != nil {
					return fmt.Errorf("saving framework overlay: %w", err)
				}
				fmt.Printf("Custom worker %q removed from global %s overlay\n", name, fwName)
			} else {
				if err := config.RemoveProjectCustomWorker(cwd, name); err != nil {
					return err
				}
				fmt.Printf("Custom worker %q removed from .lerd.yaml\n", name)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&global, "global", false, "Remove from global framework overlay instead of .lerd.yaml")
	return cmd
}

// siteFrameworkName returns the saved framework name for the given site, or "".
// Does not auto-detect — framework should already be set at link time.
func siteFrameworkName(siteName string) string {
	site, err := config.FindSite(siteName)
	if err != nil {
		return ""
	}
	return site.Framework
}

// workerNames returns the systemd unit name and the human-readable display
// site for the given (siteName, sitePath, workerName). When sitePath is a
// worktree under the parent site, both values carry a "-<wtBase>" /
// "/<wtBase>" suffix so per-worktree units don't collide with the parent's
// and CLI output can tell them apart. Single config.FindSite call serves
// both shapes — formerly two helpers each looked up independently.
func workerNames(siteName, sitePath, workerName string) (unit, display string) {
	unit = "lerd-" + workerName + "-" + siteName
	display = siteName
	if siteName == "" || workerName == "" || sitePath == "" {
		return unit, display
	}
	s, _ := config.FindSite(siteName)
	if s == nil || s.Path == "" || s.Path == sitePath {
		return unit, display
	}
	wtBase := filepath.Base(sitePath)
	return unit + "-" + config.WorktreeUnitSlug(wtBase), display + "/" + wtBase
}

// workerUnitName is a thin wrapper around workerNames for callers that only
// need the unit name (legacy callers, mostly).
func workerUnitName(siteName, sitePath, workerName string) string {
	unit, _ := workerNames(siteName, sitePath, workerName)
	return unit
}

// resolveWorkerFPMUnit returns the container name that workers for siteName
// should `podman exec` into. Three cases:
//
//   - custom container site → its own dedicated container
//   - FrankenPHP site       → its dunglas/frankenphp container
//   - everything else       → shared lerd-php<v>-fpm
//
// Centralised here because restoreWorker (linux + darwin) and the macOS
// writeWorker* helpers used to repeat the resolution and missed the
// FrankenPHP branch — workers on FrankenPHP sites ended up exec'ing into
// the shared FPM container that doesn't run their PHP at all.
func resolveWorkerFPMUnit(siteName, phpVersion string) string {
	if site, _ := config.FindSite(siteName); site != nil {
		switch {
		case site.IsHostProxy():
			// Host-proxy sites run their dev server on the host and have no FPM
			// container, so there is nothing to depend on or exec into. Returning
			// "" lets writeHostWorkerUnitFile skip the FPM ordering block.
			return ""
		case site.IsCustomContainer():
			return podman.CustomContainerName(siteName)
		case site.IsFrankenPHP():
			return podman.FrankenPHPContainerName(siteName)
		case site.IsCustomFPM():
			return podman.CustomFPMContainerName(siteName)
		}
	}
	return "lerd-php" + strings.ReplaceAll(phpVersion, ".", "") + "-fpm"
}

// WorkerStopForSite stops and removes the named worker unit for the given site.
// When sitePath is a path under a worktree (i.e. differs from the registered
// site path), the per-worktree unit is targeted instead of the parent's.
// Pass site.Path (or any path on the parent site) to stop the parent unit.
func WorkerStopForSite(siteName, sitePath, workerName string) error {
	unitName, displaySite := workerNames(siteName, sitePath, workerName)
	return stopWorkerUnit(unitName, workerName, displaySite)
}

// stopWorkerUnit tears down a fully-qualified unit name. Used by both the
// per-site stop entry point and the worktree-removal cleanup pass. Disable
// + Stop + RemoveTimerUnit + RemoveServiceUnit are run unconditionally so
// the call works regardless of whether the worker was scheduled (.timer +
// oneshot .service) or a long-running daemon (.service alone). Missing
// units are no-ops at this layer.
func stopWorkerUnit(unitName, label, displaySite string) error {
	_ = services.Mgr.Disable(unitName + ".timer")
	podman.StopUnit(unitName + ".timer") //nolint:errcheck
	_ = services.Mgr.Disable(unitName)
	podman.StopUnit(unitName) //nolint:errcheck

	if err := services.Mgr.RemoveTimerUnit(unitName); err != nil {
		return fmt.Errorf("removing timer unit file: %w", err)
	}
	if err := services.Mgr.RemoveServiceUnit(unitName); err != nil {
		return fmt.Errorf("removing unit file: %w", err)
	}
	// Drop the macOS exec-mode guard script + pid file (no-op on Linux).
	// Without this they linger in ~/.local/share/lerd/run/workers after
	// a normal stop and confuse later mode-migration discovery.
	removeWorkerExecArtifacts(unitName)
	if err := podman.DaemonReloadFn(); err != nil {
		fmt.Printf("[WARN] daemon-reload: %v\n", err)
	}

	if label == "" {
		label = unitName
	}
	if displaySite == "" {
		displaySite = unitName
	}
	fmt.Printf("%s stopped for %s\n", label, displaySite)
	return nil
}

// StopAllWorkersForWorktree stops every per-worktree worker unit attached
// to the given (site, worktree) pair. Called from `lerd worktree remove`
// and from the watcher's onRemoved hook so units don't restart-loop
// against a deleted WorkingDirectory after the user tears down a worktree.
// Returns the first underlying error so the caller can log it; siblings
// keep being torn down regardless.
func StopAllWorkersForWorktree(siteName, wtBase string) error {
	if siteName == "" || wtBase == "" {
		return nil
	}
	// Unit names sanitize dots, so match against the same slug used at creation.
	wtBase = config.WorktreeUnitSlug(wtBase)
	suffix := "-" + siteName + "-" + wtBase
	pattern := "lerd-*" + suffix
	units := services.Mgr.ListServiceUnits(pattern)
	displaySite := siteName + "/" + wtBase
	var firstErr error
	for _, unit := range units {
		// Defensive trim: globs can theoretically return false positives
		// or the mgr may widen the result set. Skip anything that doesn't
		// actually end in our suffix.
		if !strings.HasSuffix(unit, suffix) {
			continue
		}
		workerName := strings.TrimSuffix(strings.TrimPrefix(unit, "lerd-"), suffix)
		if workerName == "" {
			continue
		}
		if err := stopWorkerUnit(unit, workerName, displaySite); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// workerNameForSiteUnit parses a worker unit name shaped lerd-<worker>-<site> or
// lerd-<worker>-<site>-<slug> and returns <worker>. ok is false when the unit
// is not a worker unit for siteName.
func workerNameForSiteUnit(unit, siteName string) (string, bool) {
	rem, ok := strings.CutPrefix(unit, "lerd-")
	if !ok {
		return "", false
	}
	marker := "-" + siteName
	for idx := strings.Index(rem, marker); idx > 0; {
		after := rem[idx+len(marker):]
		if after == "" || strings.HasPrefix(after, "-") {
			return rem[:idx], true
		}
		next := strings.Index(rem[idx+1:], marker)
		if next < 0 {
			break
		}
		idx += 1 + next
	}
	return "", false
}

// siteOwnsWorkerUnit reports whether unit unambiguously belongs to siteName: the
// name must parse as siteName's worker unit AND no other registered site parse
// it too. Worker-unit names are ambiguous (lerd-horizon-web-feat is both web's
// "feat" worktree horizon unit and a "feat" site's "horizon-web" worker), so
// when another registered site also matches we decline rather than risk tearing
// down the wrong site's unit; the cost is at most leaving one unit behind.
func siteOwnsWorkerUnit(unit, siteName string, others []string) (string, bool) {
	worker, ok := workerNameForSiteUnit(unit, siteName)
	if !ok {
		return "", false
	}
	for _, o := range others {
		if o == siteName {
			continue
		}
		if _, also := workerNameForSiteUnit(unit, o); also {
			return "", false
		}
	}
	return worker, true
}

// stopAllSiteWorkerUnits stops and removes every worker unit for a site, parent
// and per-worktree, by listing units rather than walking git, so it works even
// after the site path is deleted (watcher prune) when worktree detection can't
// run. Only units siteOwnsWorkerUnit confirms are unambiguously this site's are
// torn down.
func stopAllSiteWorkerUnits(site *config.Site) {
	var others []string
	if reg, err := config.LoadSites(); err == nil {
		for _, s := range reg.Sites {
			if s.Name != "" && s.Name != site.Name {
				others = append(others, s.Name)
			}
		}
	}
	seen := map[string]bool{}
	for _, glob := range []string{"lerd-*-" + site.Name, "lerd-*-" + site.Name + "-*"} {
		for _, unit := range services.Mgr.ListServiceUnits(glob) {
			if seen[unit] {
				continue
			}
			worker, ok := siteOwnsWorkerUnit(unit, site.Name, others)
			if !ok {
				continue
			}
			seen[unit] = true
			_ = stopWorkerUnit(unit, worker, site.Name)
		}
	}
}

// isServiceActiveOrRestarting returns true if the unit is active or activating.
func isServiceActiveOrRestarting(name string) bool {
	status, _ := podman.UnitStatus(name)
	return status == "active" || status == "activating"
}

// findOrphanedWorkers returns worker names that are running but not in the known set.
func findOrphanedWorkers(siteName string, known map[string]bool) []string {
	suffix := "-" + siteName
	prefix := "lerd-"
	units := services.Mgr.ListServiceUnits("lerd-*-" + siteName)
	var sites []config.Site
	if reg, err := config.LoadSites(); err == nil {
		sites = reg.Sites
	}
	// A host-proxy site's dev server (lerd-app-<site>) is the main process, not
	// an orphan; handled here so callers don't each special-case it.
	hostProxySite := false
	for _, s := range sites {
		if s.Name == siteName && s.IsHostProxy() {
			hostProxySite = true
			break
		}
	}
	var orphans []string
	for _, unit := range units {
		workerName := strings.TrimPrefix(unit, prefix)
		workerName = strings.TrimSuffix(workerName, suffix)
		if workerName == "" || known[workerName] {
			continue
		}
		if hostProxySite && workerName == config.HostProxyWorkerName {
			continue
		}
		switch workerName {
		case "php84-fpm", "php83-fpm", "php82-fpm", "php81-fpm", "php80-fpm",
			"nginx", "dns", "dns-forwarder", "watcher", "ui", "stripe":
			continue
		}
		if lerdSystemd.UnitBelongsToOtherSiteWorktree(workerName, siteName, sites) {
			continue
		}
		if isServiceActiveOrRestarting(unit) {
			orphans = append(orphans, workerName)
		}
	}
	return orphans
}
